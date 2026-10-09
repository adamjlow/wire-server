//go:build schemacheck

// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"
)

func TestScheduledExecutionDockerStorage(t *testing.T) {
	if os.Getenv("WIRE_SCHEMA_CHECK_SYNTHETIC") != "1" {
		t.Fatal("requires owned synthetic Docker fixture")
	}
	ctx, stop := context.WithTimeout(t.Context(), time.Minute)
	defer stop()
	galley := GalleyConfig{Postgres: map[string]string{"host": "postgres", "port": "5432", "user": "postgres", "password": "synthetic-schema-only", "dbname": "backendA", "sslmode": "disable"}}
	config := PostgresPoolConfig{Size: 4, Acquisition: Duration(5 * time.Second), Idleness: Duration(time.Minute)}
	if err := runScheduledSchema(ctx, config, galley); err != nil {
		t.Fatal(err)
	}
	store, err := openPostgres(ctx, config, galley)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Pool.Close()
	exec := func(sql string) {
		t.Helper()
		if _, err := store.Pool.Exec(ctx, sql); err != nil {
			t.Fatal("fixture setup failed")
		}
	}
	count := func(sql string, want int64) {
		t.Helper()
		var got int64
		if err := store.Pool.QueryRow(ctx, sql).Scan(&got); err != nil || got != want {
			t.Fatal("durable control state mismatch", sql, got, want, err)
		}
	}
	exec(`CREATE TABLE synthetic_effects (id bigint PRIMARY KEY);
 INSERT INTO arbiter.meetings (id,payload,attempts,archive_for,max_attempts,parent_id,suspended,cancel_requested_at,parent_state) VALUES
 (1,'{"type":"meetings_cleanup","data":{}}',1,60,3,NULL,false,NULL,NULL),
 (2,'{}',1,NULL,3,NULL,false,NULL,NULL),
 (3,'{}',3,NULL,3,NULL,false,NULL,NULL),
 (4,'{}',2,NULL,3,NULL,false,NULL,NULL),
 (6,'{}',0,NULL,3,NULL,true,NULL,NULL),
 (5,'{}',1,NULL,3,6,false,NOW(),NULL),
 (500,'{}',10,NULL,NULL,NULL,false,NULL,'{}'),
 (501,'{}',0,NULL,NULL,500,true,NULL,'{"999":{"Right":"snapshot"}}'),
 (502,'{}',0,NULL,NULL,501,false,NULL,NULL);
 INSERT INTO arbiter.meetings_results(parent_id,child_id,result) VALUES(500,900,'"current-root-result"'),(501,777,'"current-child-result"');
 INSERT INTO arbiter.meetings_dlq(job_id,payload,parent_id,last_error) VALUES(777,'{}',501,'stored-child-error');`)
	job := claimedScheduledJob{ID: 1, Attempts: 1, ArchiveFor: ptr(int32(60)), MaxAttempts: ptr(int32(3))}
	if err := store.CompleteScheduledJob(ctx, "meetings", job, func(context.Context) error {
		count(`SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND state='idle in transaction'`, 1)
		exec(`INSERT INTO synthetic_effects VALUES(1)`)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	count(`SELECT count(*) FROM arbiter.meetings WHERE id=1`, 0)
	count(`SELECT count(*) FROM arbiter.meetings_archive WHERE job_id=1 AND result IS NULL`, 1)
	count(`SELECT count(*) FROM arbiter.meetings_results WHERE child_id=1`, 0)
	job = claimedScheduledJob{ID: 2, Attempts: 1, MaxAttempts: ptr(int32(3))}
	handlerErr := errors.New("synthetic private failure")
	if err := store.CompleteScheduledJob(ctx, "meetings", job, func(context.Context) error { exec(`INSERT INTO synthetic_effects VALUES(2)`); return handlerErr }); !errors.Is(err, handlerErr) {
		t.Fatal("handler failure swallowed")
	}
	count(`SELECT count(*) FROM synthetic_effects`, 2)
	count(`SELECT count(*) FROM arbiter.meetings WHERE id=2 AND last_error IS NULL`, 1)
	if changed, err := store.FailScheduledJob(ctx, "meetings", job, 4); err != nil || !changed {
		t.Fatal("retry failed", err)
	}
	count(`SELECT count(*) FROM arbiter.meetings WHERE id=2 AND attempts=1 AND claimed_by IS NULL AND last_error='scheduled job failed' AND not_visible_until BETWEEN NOW()+interval '2 seconds' AND NOW()+interval '5 seconds'`, 1)
	job = claimedScheduledJob{ID: 3, Attempts: 3, MaxAttempts: ptr(int32(3))}
	if changed, err := store.FailScheduledJob(ctx, "meetings", job, 4); err != nil || !changed {
		t.Fatal("DLQ failed", err)
	}
	count(`SELECT count(*) FROM arbiter.meetings_dlq WHERE job_id=3 AND attempts=3 AND max_attempts=3 AND last_error='scheduled job failed'`, 1)
	job = claimedScheduledJob{ID: 4, Attempts: 1, MaxAttempts: ptr(int32(3))}
	if err := store.CompleteScheduledJob(ctx, "meetings", job, func(context.Context) error { return nil }); !errors.Is(err, errScheduledGone) {
		t.Fatal("stale completion accepted", err)
	}
	if changed, err := store.FailScheduledJob(ctx, "meetings", job, 4); err != nil || changed {
		t.Fatal("stale retry changed new attempt", err)
	}
	count(`SELECT count(*) FROM arbiter.meetings WHERE id=4 AND attempts=2 AND last_error IS NULL`, 1)
	if err := store.DeleteCancelledScheduledJob(ctx, "meetings", 5); err != nil {
		t.Fatal(err)
	}
	count(`SELECT count(*) FROM arbiter.meetings WHERE id=5`, 0)
	count(`SELECT count(*) FROM arbiter.meetings WHERE id=6 AND NOT suspended`, 1)
	if err := store.DeleteCancelledScheduledJob(ctx, "meetings", 4); err != nil {
		t.Fatal(err)
	}
	count(`SELECT count(*) FROM arbiter.meetings WHERE id=4`, 1)
	job = claimedScheduledJob{ID: 500, Attempts: 10, ParentState: json.RawMessage(`{}`)}
	if changed, err := store.FailScheduledJob(ctx, "meetings", job, 4); err != nil || !changed {
		t.Fatal("rollup DLQ failed", err)
	}
	count(`SELECT count(*) FROM arbiter.meetings_dlq WHERE job_id IN (500,501,502)`, 3)
	count(`SELECT count(*) FROM arbiter.meetings_dlq WHERE job_id=500 AND parent_state->'900'->>'Right'='current-root-result'`, 1)
	count(`SELECT count(*) FROM arbiter.meetings_dlq WHERE job_id=501 AND parent_state->'777'->>'Left'='stored-child-error' AND parent_state->'999'->>'Right'='snapshot'`, 1)
}
