//go:build schemacheck

// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Uses the unchanged native schema, and never connects to host/shared services.
func TestScheduledPoolDockerStorage(t *testing.T) {
	if os.Getenv("WIRE_SCHEMA_CHECK_SYNTHETIC") != "1" {
		t.Fatal("requires owned synthetic Docker fixture")
	}
	ctx, stop := context.WithTimeout(t.Context(), time.Minute)
	defer stop()
	galley := GalleyConfig{Postgres: map[string]string{"host": "postgres", "port": "5432", "user": "postgres", "password": "synthetic-schema-only", "dbname": "backendA", "sslmode": "disable"}}
	pc := PostgresPoolConfig{Size: 8, Acquisition: Duration(5 * time.Second), Idleness: Duration(time.Minute)}
	if err := runScheduledSchema(ctx, pc, galley); err != nil {
		t.Fatal(err)
	}
	store, err := openPostgres(ctx, pc, galley)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Pool.Close()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := store.Pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal("fixture SQL failed", err)
		}
	}
	count := func(sql string, want int64) {
		t.Helper()
		var got int64
		if err := store.Pool.QueryRow(ctx, sql).Scan(&got); err != nil || got != want {
			t.Fatal("durable state mismatch", sql, got, want, err)
		}
	}
	exec(`INSERT INTO arbiter.meetings(id,payload) VALUES (1,'{"type":"meetings_cleanup","data":{}}'),(2,'{}')`)
	if jobs, err := store.ClaimScheduledJobs(ctx, "meetings", jobUUID, time.Minute, 2); err == nil || len(jobs) != 0 {
		t.Fatal("malformed typed claim accepted")
	}
	count(`SELECT count(*) FROM arbiter.meetings WHERE attempts=0 AND claimed_by IS NULL AND not_visible_until IS NULL`, 2)
	exec(`DELETE FROM arbiter.meetings WHERE id=2`)
	jobs, err := store.ClaimScheduledJobs(ctx, "meetings", jobUUID, time.Minute, 2)
	if err != nil || len(jobs) != 1 || jobs[0].Attempts != 1 {
		t.Fatal("valid typed claim failed", err)
	}
	count(`SELECT count(*) FROM arbiter.meetings WHERE id=1 AND claimed_by='`+jobUUID+`' AND attempts=1`, 1)
	if err := store.CompleteScheduledJob(ctx, "meetings", jobs[0], func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}

	c := scheduledPoolFixtureConfig()
	c.Jobs.WorkerHeartbeat = Duration(30 * time.Millisecond)
	c.Jobs.JobHeartbeat = Duration(50 * time.Millisecond)
	c.Jobs.Visibility = Duration(5 * time.Second)
	grace := Duration(50 * time.Millisecond)
	c.Jobs.Grace = &grace
	started := make(chan struct{}, 4)
	registry := make(chan struct{}, 1)
	results := make(chan scheduledExecutionOutcome, 4)
	pool, err := newScheduledPool(store, "meetings", jobUUID, c, func(ctx context.Context, _ scheduledJobPayload) error {
		started <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	pool.Liveness = filepath.Join(t.TempDir(), "liveness")
	pool.Report = func(event string, result scheduledExecutionOutcome) {
		if event == "registry" && result.Err == nil {
			signalScheduled(registry)
		}
		if event == "job" {
			results <- result
		}
	}
	exec(`INSERT INTO arbiter.arbiter_queues(queue_name,paused) VALUES('meetings',true); INSERT INTO arbiter.meetings(id,payload) VALUES (10,'{"type":"meetings_cleanup","data":{}}'),(11,'{"type":"meetings_cleanup","data":{}}')`)
	poolCtx, poolStop := context.WithCancel(ctx)
	defer poolStop()
	poolDone := make(chan error, 1)
	go func() { poolDone <- pool.Run(poolCtx) }()
	awaitScheduledTestSignal(t, registry)
	count(`SELECT count(*) FROM arbiter.meetings WHERE attempts=0 AND claimed_by IS NULL`, 2)
	count(`SELECT count(*) FROM arbiter.arbiter_workers WHERE worker_id='`+jobUUID+`' AND worker_count=2 AND NOT shutting_down`, 1)
	if _, err := os.Stat(pool.Liveness); err != nil {
		t.Fatal("liveness absent", err)
	}
	listenerCfg := store.Pool.Config().ConnConfig.Copy()
	listenerCfg.RuntimeParams["application_name"] = "synthetic-scheduled-listener"
	listenerCtx, listenerStop := context.WithCancel(ctx)
	defer listenerStop()
	ready := make(chan bool, 4)
	disconnected := make(chan struct{}, 1)
	listenerDone := make(chan struct{})
	go func() {
		defer close(listenerDone)
		runScheduledListener(listenerCtx, listenerCfg, []*scheduledPool{pool}, func(value bool) { ready <- value }, func(error) { signalScheduled(disconnected) })
	}()
	for _, want := range []bool{false, true} {
		select {
		case got := <-ready:
			if got != want {
				t.Fatal("listener readiness mismatch")
			}
		case <-time.After(3 * time.Second):
			t.Fatal("listener not ready")
		}
	}
	exec(`SELECT pg_notify('arbiter_cron_run_arbiter','meetings-cleanup')`)
	awaitScheduledTestSignal(t, pool.cronWake)
	exec(`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE application_name='synthetic-scheduled-listener'`)
	awaitScheduledTestSignal(t, disconnected)
	// Reconnect is asynchronous. Probe the existing manual cron channel until the
	// new subscription receives one; a live connection alone is insufficient.
	reconnectTimer := time.NewTimer(4 * time.Second)
	defer reconnectTimer.Stop()
	reconnectTick := time.NewTicker(100 * time.Millisecond)
	defer reconnectTick.Stop()
	reconnected := false
	for !reconnected {
		select {
		case <-reconnectTimer.C:
			t.Fatal("LISTEN subscription not restored")
		case <-reconnectTick.C:
			exec(`SELECT pg_notify('arbiter_cron_run_arbiter','meetings-cleanup')`)
		case <-pool.cronWake:
			reconnected = true
		}
	}
	select {
	case value := <-ready:
		t.Fatal("sticky listener readiness changed on reconnect", value)
	default:
	}
	exec(`UPDATE arbiter.arbiter_queues SET paused=false WHERE queue_name='meetings'; SELECT pg_notify('arbiter_pause_arbiter_meetings','{"worker_id":"` + jobUUID + `","paused":false}')`)
	awaitScheduledTestSignal(t, started)
	awaitScheduledTestSignal(t, started)
	count(`SELECT count(*) FROM arbiter.meetings WHERE attempts=1 AND claimed_by='`+jobUUID+`'`, 2)
	// A missing registry row is restored by the heartbeat/proof-of-work loop.
	exec(`DELETE FROM arbiter.arbiter_workers WHERE worker_id='` + jobUUID + `'`)
	deadline := time.Now().Add(3 * time.Second)
	for {
		var n int
		if err := store.Pool.QueryRow(ctx, `SELECT count(*) FROM arbiter.arbiter_workers WHERE worker_id=$1::uuid`, jobUUID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("worker not re-registered")
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	exec(`UPDATE arbiter.meetings SET cancel_requested_at=NOW() WHERE id=10; SELECT pg_notify('arbiter_cancel_arbiter_meetings','{"worker_id":"` + jobUUID + `","job_id":10}')`)
	select {
	case outcome := <-results:
		if outcome.Status != "cancelled" || outcome.Err != nil {
			t.Fatal("force cancel failed", outcome)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("force cancel not delivered")
	}
	count(`SELECT count(*) FROM arbiter.meetings WHERE id=10`, 0)
	poolStop()
	select {
	case err := <-poolDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("finite drain did not terminate")
	}
	count(`SELECT count(*) FROM arbiter.arbiter_workers WHERE worker_id='`+jobUUID+`'`, 0)
	count(`SELECT count(*) FROM arbiter.meetings WHERE id=11 AND attempts=1 AND claimed_by='`+jobUUID+`' AND not_visible_until>NOW() AND last_error IS NULL`, 1)
	count(`SELECT count(*) FROM arbiter.meetings_dlq WHERE job_id=11`, 0)
	if _, err := os.Stat(pool.Liveness); !os.IsNotExist(err) {
		t.Fatal("liveness survived drain")
	}
	listenerStop()
	select {
	case <-listenerDone:
	case <-time.After(3 * time.Second):
		t.Fatal("listener did not stop")
	}
}
