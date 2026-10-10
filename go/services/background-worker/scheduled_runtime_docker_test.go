//go:build schemacheck

// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestScheduledDomainDockerRuntime(t *testing.T) {
	if os.Getenv("WIRE_SCHEMA_CHECK_SYNTHETIC") != "1" {
		t.Fatal("requires owned synthetic Docker fixture")
	}
	ctx, stop := context.WithTimeout(t.Context(), time.Minute)
	defer stop()
	g := GalleyConfig{Postgres: map[string]string{"host": "postgres", "port": "5432", "user": "postgres", "password": "synthetic-schema-only", "dbname": "backendA", "sslmode": "disable"}}
	g.Settings.Domain = "example.com"
	pc := PostgresPoolConfig{Size: 8, Acquisition: Duration(5 * time.Second), Idleness: Duration(time.Minute)}
	if err := runScheduledSchema(ctx, pc, g); err != nil {
		t.Fatal(err)
	}
	pg, err := openPostgres(ctx, pc, g)
	if err != nil {
		t.Fatal(err)
	}
	defer pg.Pool.Close()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pg.Pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal("native fixture statement failed", err)
		}
	}
	names := []string{"20250908142515-create-conversation-tables.sql", "20260121092128-history_metadata.sql", "20260123124917-team-features.sql", "20251213223355-create-meetings-table.sql", "20260115150600-conversation-codes.sql", "20260619120000-conversation-codes-target.sql", "20260708090000-meetings-recurrence-eff-end-index.sql", "20260708100000-meetings-end-time-nonrecurring-index.sql", "20260713120000-meetings-recurrence-consistency-constraint.sql", "20260729120000-meetings-tzid.sql", "20260729120500-meetings-tzid-notnull.sql", "20260923120000-meetings-mtype.sql"}
	for _, name := range names {
		body, err := os.ReadFile(filepath.Join("/reference/migrations", name))
		if err != nil {
			t.Fatal("native schema unavailable")
		}
		exec(string(body))
	}
	c := scheduledPoolFixtureConfig()
	c.Jobs.Poll = Duration(10 * time.Millisecond)
	c.Jobs.ReaperInterval = Duration(time.Hour)
	c.Jobs.ReaperTimeout = Duration(time.Second)
	c.Storage.Conversation = storagePostgres
	c.Storage.Codes = storagePostgres
	c.Storage.TeamFeatures = storagePostgres
	c.Meetings = MeetingsCleanupConfig{RetentionHours: 1, BatchSize: 2, Schedule: "0 0 1 1 *"}
	service := New(c, g)
	queue := &observedNotificationQueue{}
	pools, err := service.scheduledPools(pg, cassandraConversationStore{}, nil, queue, &http.Client{}, notificationClient())
	if err != nil {
		t.Fatal(err)
	}
	for _, pool := range pools {
		pool.Liveness = filepath.Join(t.TempDir(), pool.Queue)
	}
	// The concrete meeting handler must remove an expired meeting/code even when
	// its conversation is a regular group rather than a meeting. Keep that group.
	// Adminless jobs read the same real group and disabled server feature default.
	exec(`INSERT INTO conversation(id,type,protocol,group_conv_type,team) VALUES('00000000-0000-0000-0000-000000000098',0,0,0,'00000000-0000-0000-0000-000000000099')`)
	exec(`INSERT INTO meetings(id,title,creator,start_time,end_time,tzid,conversation_id) VALUES('00000000-0000-0000-0000-000000000001','synthetic','00000000-0000-0000-0000-000000000099',NOW()-INTERVAL '3 hours',NOW()-INTERVAL '2 hours','UTC','00000000-0000-0000-0000-000000000098')`)
	key, err := meetingCodeKey("00000000-0000-0000-0000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO conversation_codes(key,conversation,value,expires_at,target) VALUES($1,'00000000-0000-0000-0000-000000000001','synthetic',NOW()+INTERVAL '1 hour','meeting')`, key)
	exec(`INSERT INTO arbiter.meetings(payload) VALUES('{"type":"meetings_cleanup","data":{}}');
 INSERT INTO arbiter.conversations(payload) VALUES('{"type":"adminless_setup","data":{"team_id":"00000000-0000-0000-0000-000000000099","request_id":"synthetic-job"}}'),('{"type":"adminless_deletion","data":{"team_id":"00000000-0000-0000-0000-000000000099","conversation_id":"00000000-0000-0000-0000-000000000098","request_id":"synthetic-job"}}')`)
	runtimeCtx, cancelRuntime := context.WithCancel(ctx)
	defer cancelRuntime()
	runtime := &scheduledRuntime{Service: service, Postgres: pg, Pools: pools}
	done := make(chan struct{})
	go func() { defer close(done); runtime.Run(runtimeCtx) }()
	defer func() {
		cancelRuntime()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
		}
	}()
	await := func(sql string, want int64) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			var got int64
			if err := pg.Pool.QueryRow(ctx, sql).Scan(&got); err != nil {
				t.Fatal("runtime state read failed", err)
			}
			if got == want {
				return
			}
			if time.Now().After(deadline) {
				t.Fatal("runtime state did not converge", sql, got, want)
			}
			select {
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
	await(`SELECT count(*) FROM arbiter.meetings`, 0)
	await(`SELECT count(*) FROM arbiter.conversations`, 0)
	await(`SELECT count(*) FROM meetings`, 0)
	await(`SELECT count(*) FROM conversation`, 1)
	await(`SELECT count(*) FROM conversation_codes`, 0)
	await(`SELECT count(*) FROM arbiter.arbiter_workers`, 2)
	await(`SELECT count(*) FROM arbiter.cron_schedules WHERE name='meetings-cleanup'`, 1)
	await(`SELECT count(*) FROM arbiter.arbiter_gates`, 5)
	r := httptest.NewRecorder()
	service.Handler().ServeHTTP(r, httptest.NewRequest("GET", "/i/metrics", nil))
	if !strings.Contains(r.Body.String(), "wire_meetings_cleanup_runs_total 1\n") {
		t.Fatal("native cleanup run counter absent")
	}
	readyDeadline := time.Now().Add(3 * time.Second)
	for !runtime.listenerReady.Load() {
		if time.Now().After(readyDeadline) {
			t.Fatal("shared listener did not subscribe")
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	exec(`UPDATE arbiter.cron_schedules SET run_requested_at=NOW() WHERE name='meetings-cleanup'; SELECT pg_notify('arbiter_cron_run_arbiter','meetings-cleanup')`)
	deadline := time.Now().Add(5 * time.Second)
	for {
		service.mu.Lock()
		runs := service.meetingsCleanupRuns
		service.mu.Unlock()
		if runs == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("manual cron did not reach domain handler")
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancelRuntime()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("combined scheduled runtime did not stop")
	}
	await(`SELECT count(*) FROM arbiter.arbiter_workers`, 0)
	await(`SELECT count(*) FROM arbiter.meetings_dlq`, 0)
	await(`SELECT count(*) FROM arbiter.conversations_dlq`, 0)
	for _, pool := range pools {
		if _, err := os.Stat(pool.Liveness); !os.IsNotExist(err) {
			t.Fatal("runtime liveness survived shutdown")
		}
	}
	// PostgreSQL-only routes, disabled adminless feature, no Brig/bot/Rabbit
	// calls: this fixture qualifies integration boundaries, not whole-service E2E.
}
