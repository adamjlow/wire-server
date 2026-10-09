//go:build schemacheck

// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// Explicit Docker-only qualifier, excluded from ordinary unit/race checks.
// Fixed isolated fixture target; never accept a user-provided database URL.
func TestScheduledSchemaDockerStorage(t *testing.T) {
	if os.Getenv("WIRE_SCHEMA_CHECK_SYNTHETIC") != "1" {
		t.Fatal("schema qualifier requires the owned synthetic Docker fixture")
	}
	ctx, stop := context.WithTimeout(t.Context(), time.Minute)
	defer stop()
	g := GalleyConfig{Postgres: map[string]string{"host": "postgres", "port": "5432", "user": "postgres", "password": "synthetic-schema-only", "dbname": "backendA", "sslmode": "disable"}}
	config := PostgresPoolConfig{Size: 2, Acquisition: Duration(5 * time.Second), Idleness: Duration(time.Minute)}
	if err := runScheduledSchema(ctx, config, g); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, "host=postgres port=5432 user=postgres password=synthetic-schema-only dbname=backendA sslmode=disable")
	if err != nil {
		t.Fatal("fixture database unavailable")
	}
	defer func() { _ = conn.Close(context.Background()) }()
	var actualJSON []byte
	if err := conn.QueryRow(ctx, `SELECT coalesce(json_agg(row_to_json(c) ORDER BY table_name,ordinal_position),'[]') FROM information_schema.columns c WHERE table_schema='arbiter'`).Scan(&actualJSON); err != nil {
		t.Fatal("schema inspection failed")
	}
	expectedJSON, err := os.ReadFile("/reference/columns.json")
	if err != nil {
		t.Fatal("native captured schema columns unavailable")
	}
	var actual, expected []map[string]any
	if json.Unmarshal(actualJSON, &actual) != nil || json.Unmarshal(expectedJSON, &expected) != nil {
		t.Fatal("invalid schema column capture")
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("native observed schema columns differ: got%d expected%d", len(actual), len(expected))
	}
	var count int
	if conn.QueryRow(ctx, `SELECT count(*) FROM arbiter.schema_migrations`).Scan(&count) != nil || count != 76 {
		t.Fatal("native migration ledger incomplete")
	}
	// Native group triggers must keep queue/group state useful to either worker.
	if _, err := conn.Exec(ctx, `INSERT INTO arbiter.meetings (payload,group_key) VALUES ('{"type":"meetings_cleanup","data":{}}','synthetic-group')`); err != nil {
		t.Fatal("native queue/group trigger insert failed")
	}
	if err := runScheduledSchema(ctx, config, g); err != nil {
		t.Fatal("existing scheduled schema did not resume")
	}
	if conn.QueryRow(ctx, `SELECT count(*) FROM arbiter.meetings WHERE group_key='synthetic-group'`).Scan(&count) != nil || count != 1 {
		t.Fatal("bootstrap rerun lost queued workload")
	}
	// Reject a corrupt existing checksum while preserving the native commit of
	// an earlier missing entry. No fixture/expectation is modified by this probe.
	if _, err := conn.Exec(ctx, `DELETE FROM arbiter.schema_migrations WHERE filename='create-cron-schedules'; UPDATE arbiter.schema_migrations SET checksum='synthetic-corrupt-checksum' WHERE filename='conversations-create-notify-trigger'`); err != nil {
		t.Fatal("fixture mismatch setup failed")
	}
	var mismatch *scheduledSchemaMismatch
	if err := runScheduledSchema(ctx, config, g); !errors.As(err, &mismatch) {
		t.Fatal("native checksum mismatch did not reject bootstrap")
	}
	if conn.QueryRow(ctx, `SELECT count(*) FROM arbiter.schema_migrations WHERE filename='create-cron-schedules'`).Scan(&count) != nil || count != 1 {
		t.Fatal("native partial migration commit behavior changed")
	}
	t.Logf("native schema columns%d; ledger76; queued workload preserved on rerun; mismatch stops with native preceding commit", len(actual))
}

func TestScheduledReaperDockerStorage(t *testing.T) {
	if os.Getenv("WIRE_SCHEMA_CHECK_SYNTHETIC") != "1" {
		t.Fatal("reaper qualifier requires owned synthetic Docker")
	}
	ctx, stop := context.WithTimeout(t.Context(), time.Minute)
	defer stop()
	g := GalleyConfig{Postgres: map[string]string{"host": "postgres", "port": "5432", "user": "postgres", "password": "synthetic-schema-only", "dbname": "backendA", "sslmode": "disable"}}
	c := PostgresPoolConfig{Size: 4, Acquisition: Duration(5 * time.Second), Idleness: Duration(time.Minute)}
	if err := runScheduledSchema(ctx, c, g); err != nil {
		t.Fatal(err)
	}
	store, err := openPostgres(ctx, c, g)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Pool.Close()
	conn, err := store.acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	seed := `INSERT INTO arbiter.meetings (id,payload,suspended,parent_id,attempts,parent_state,not_visible_until,cancel_requested_at,group_key) VALUES
 (100,'{}',true,NULL,0,NULL,NULL,NULL,NULL),
 (101,'{}',false,100,0,NULL,NOW()-interval '1 hour',NOW()-interval '1 hour',NULL),
 (103,'{}',false,NULL,0,NULL,NOW()+interval '1 hour',NOW(),NULL),
 (200,'{}',false,NULL,10,'{}',NULL,NULL,NULL),
 (201,'{}',true,200,0,'{"300":{"Right":"snapshot"}}',NULL,NULL,NULL),
 (202,'{}',false,201,0,NULL,NULL,NULL,NULL),
 (250,'{}',false,NULL,10,NULL,NOW()+interval '1 hour',NULL,NULL),
 (252,'{}',true,NULL,10,NULL,NULL,NULL,NULL),
 (300,'{}',false,NULL,0,NULL,NULL,NULL,'ready-group');
 INSERT INTO arbiter.meetings_results (parent_id,child_id,result) VALUES (201,300,'"result"');
 INSERT INTO arbiter.meetings_dlq (job_id,payload,parent_id,last_error) VALUES (300,'{}',201,'stored-error');
 INSERT INTO arbiter.meetings_archive (job_id,payload,archive_expires_at) VALUES (1,'{}',NOW()-interval '1 hour'),(2,'{}',NOW()+interval '1 hour');
 UPDATE arbiter.meetings_groups SET job_count=99 WHERE group_key='ready-group';
 INSERT INTO arbiter.arbiter_workers (worker_id,queue_name,paused,last_heartbeat,stale_threshold_secs) VALUES
 ('00000000-0000-0000-0000-000000000001','meetings',true,NOW()-interval '1 hour',1),
 ('00000000-0000-0000-0000-000000000002','meetings',false,NOW(),3600);`
	if _, err := conn.Exec(ctx, seed); err != nil {
		t.Fatal("reaper fixture seed failed")
	}
	results := store.reapScheduledJobs(ctx, time.Hour, time.Second)
	for _, result := range results {
		if result.Err != nil || !result.Ran || len(result.FailedQueues) != 0 {
			t.Fatalf("native reaper operation failed: %s: %v", result.Task, result.Err)
		}
	}
	checks := map[string]int64{
		`SELECT count(*) FROM arbiter.arbiter_workers`:                                             1,
		`SELECT count(*) FROM arbiter.meetings_archive`:                                            1,
		`SELECT count(*) FROM arbiter.meetings WHERE id=101`:                                       0,
		`SELECT count(*) FROM arbiter.meetings WHERE id IN (103,250,252)`:                          3,
		`SELECT count(*) FROM arbiter.meetings WHERE id=100 AND NOT suspended`:                     1,
		`SELECT count(*) FROM arbiter.meetings WHERE id IN (200,201,202)`:                          0,
		`SELECT count(*) FROM arbiter.meetings_dlq WHERE job_id IN (200,201,202)`:                  3,
		`SELECT job_count FROM arbiter.meetings_groups WHERE group_key='ready-group'`:              1,
		`SELECT count(*) FROM arbiter.arbiter_gates WHERE last_run_at > NOW()-interval '1 minute'`: 5,
	}
	for query, want := range checks {
		var got int64
		if conn.QueryRow(ctx, query).Scan(&got) != nil || got != want {
			t.Fatal("native reaper state differs:", query, "got", got, "want", want)
		}
	}
	var snapshot string
	if conn.QueryRow(ctx, `SELECT parent_state->'300'->>'Left' FROM arbiter.meetings_dlq WHERE job_id=201`).Scan(&snapshot) != nil || snapshot != "stored-error" {
		t.Fatal("cascade deleted descendant rollup snapshot or lost error precedence")
	}
	for _, result := range store.reapScheduledJobs(ctx, time.Hour, time.Second) {
		if result.Err != nil || result.Ran {
			t.Fatal("shared native reaper interval did not suppress rerun")
		}
	}
	started, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := store.runScheduledGate(ctx, "synthetic-mutual-exclusion", time.Hour, time.Second, func(pgx.Tx) error {
			close(started)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		done <- err
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("gate winner did not start")
	}
	ran, err := store.runScheduledGate(ctx, "synthetic-mutual-exclusion", time.Hour, time.Second, func(pgx.Tx) error { t.Error("concurrent gate loser ran work"); return nil })
	close(release)
	if err != nil || ran || <-done != nil {
		t.Fatal("native gate mutual exclusion failed")
	}
	_, err = store.runScheduledGate(ctx, "synthetic-statement-timeout", time.Hour, 5*time.Millisecond, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, `SELECT pg_sleep(0.05)`); return err })
	if err == nil {
		t.Fatal("native per-statement timeout did not abort work")
	}
	ran, err = store.runScheduledGate(ctx, "synthetic-statement-timeout", time.Hour, time.Second, func(pgx.Tx) error { return nil })
	if err != nil || !ran {
		t.Fatal("failed gated work advanced watermark or leaked timeout")
	}
	t.Log("five reaper operations: lease/suspension guards, paused stale worker sweep, group repair, parent wake, tree-DLQ snapshots, archive retention, shared gate exclusion/interval and timeout rollback passed")
}
