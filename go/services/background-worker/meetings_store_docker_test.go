//go:build schemacheck

// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestMeetingsCleanupDockerStorage(t *testing.T) {
	if os.Getenv("WIRE_SCHEMA_CHECK_SYNTHETIC") != "1" {
		t.Fatal("meeting qualifier requires owned synthetic Docker")
	}
	ctx, stop := context.WithTimeout(t.Context(), time.Minute)
	defer stop()
	g := GalleyConfig{Postgres: map[string]string{"host": "postgres", "port": "5432", "user": "postgres", "password": "synthetic-schema-only", "dbname": "backendA", "sslmode": "disable"}}
	c := PostgresPoolConfig{Size: 4, Acquisition: Duration(5 * time.Second), Idleness: Duration(time.Minute)}
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
	// Original migrations via readonly mount, never a simplified candidate DDL.
	for _, name := range []string{"20251213223355-create-meetings-table.sql", "20260115150600-conversation-codes.sql", "20260619120000-conversation-codes-target.sql", "20260708090000-meetings-recurrence-eff-end-index.sql", "20260708100000-meetings-end-time-nonrecurring-index.sql", "20260713120000-meetings-recurrence-consistency-constraint.sql", "20260729120000-meetings-tzid.sql", "20260729120500-meetings-tzid-notnull.sql", "20260923120000-meetings-mtype.sql"} {
		body, err := os.ReadFile(filepath.Join("/reference/migrations", name))
		if err != nil {
			t.Fatal("native fixture migration unavailable")
		}
		if _, err := conn.Exec(ctx, string(body)); err != nil {
			t.Fatal("native fixture migration failed")
		}
	}
	exec := func(sql string, args ...any) {
		if _, err := conn.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO meetings(id,title,creator,start_time,end_time,tzid,recurrence_frequency,recurrence_interval,recurrence_until,conversation_id)
 SELECT ('00000000-0000-0000-0000-'||lpad(n::text,12,'0'))::uuid,'synthetic-'||n,'00000000-0000-0000-0000-000000000099'::uuid,
 '2026-10-09 00:00Z'::timestamptz+((ending-1)||' minutes')::interval,
 '2026-10-09 00:00Z'::timestamptz+(ending||' minutes')::interval,'UTC',frequency::recurrence_frequency,
 CASE WHEN frequency IS NOT NULL THEN 1 ELSE NULL END,
 CASE WHEN untilminute IS NOT NULL THEN '2026-10-09 00:00Z'::timestamptz+(untilminute||' minutes')::interval ELSE NULL END,
 '00000000-0000-0000-0000-000000000098'::uuid
 FROM (VALUES (1,10,NULL::text,NULL::int),(2,20,NULL,NULL),(3,5,'daily',15),(4,1,'weekly',10),(5,0,'monthly',NULL),(6,5,'yearly',100),(7,100,NULL,NULL),(8,25,'daily',5),(9,200,NULL,NULL)) AS v(n,ending,frequency,untilminute)`)
	cutoff := time.Date(2026, 10, 9, 1, 40, 0, 0, time.UTC)
	meetings, err := store.OldMeetings(ctx, cutoff, 2)
	if err != nil || len(meetings) != 2 {
		t.Fatal("native two-query selection failed")
	}
	want := []string{"00000000-0000-0000-0000-000000000001", "00000000-0000-0000-0000-000000000004"}
	var got []string
	for _, m := range meetings {
		got = append(got, postgresUUIDString(m.ID))
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stable merged expiry ids %v", got)
	}
	for _, id := range []string{"00000000-0000-0000-0000-000000000001", "00000000-0000-0000-0000-000000000005"} {
		key, err := meetingCodeKey(id)
		if err != nil {
			t.Fatal(err)
		}
		exec(`INSERT INTO conversation_codes(key,conversation,value,expires_at,target) VALUES($1,$2::uuid,'synthetic',NOW()+INTERVAL '1 hour','meeting')`, key, id)
	}
	exec(`INSERT INTO conversation_codes(key,conversation,value,expires_at,target) VALUES('synthetic-unrelated','00000000-0000-0000-0000-000000000099','synthetic',NOW()+INTERVAL '1 hour','conv')`)
	// Full selected-row decoding must fail even for metadata not used by cleanup.
	exec(`UPDATE meetings SET tzid='invalid/synthetic' WHERE id='00000000-0000-0000-0000-000000000003'`)
	if _, err := store.OldMeetings(ctx, cutoff, 2); err == nil {
		t.Fatal("malformed recurring branch row hidden by global batch cap")
	}
	exec(`UPDATE meetings SET tzid='UTC' WHERE id='00000000-0000-0000-0000-000000000003'`)
	effects := &cleanupFixture{} // Missing conversations; actual code/meeting SQL effects.
	result, err := runMeetingsCleanup(ctx, routedMeetingsCleanupStore{Postgres: store, Codes: storageMigrating}, effects, cutoff, 2, nil)
	if err != nil || result.BatchError != nil || !result.Completed || result.TotalDeleted != 5 {
		t.Fatalf("cleanup result %+v error%v", result, err)
	}
	var count int
	if conn.QueryRow(ctx, `SELECT count(*) FROM meetings`).Scan(&count) != nil || count != 4 {
		t.Fatal("open/future/boundary meetings not preserved")
	}
	if conn.QueryRow(ctx, `SELECT count(*) FROM conversation_codes`).Scan(&count) != nil || count != 2 {
		t.Fatal("meeting code deleted incorrectly")
	}
	expectedCalls := []string{"conversation", "conversation", "conversation", "conversation", "conversation"}
	if !reflect.DeepEqual(effects.calls, expectedCalls) {
		t.Fatal("unexpected deletion for absent conversations")
	}
	result, err = runMeetingsCleanup(ctx, routedMeetingsCleanupStore{Postgres: store, Codes: storageMigrating}, effects, cutoff, 2, nil)
	if err != nil || !result.Completed || result.TotalDeleted != 0 {
		t.Fatal("cleanup did not resume idempotently")
	}

	// Cassandra-only code mode explicitly leaves any PostgreSQL meeting code
	// untouched, while meetings themselves remain Postgres-owned and deletable.
	id := "00000000-0000-0000-0000-000000000001"
	key, err := meetingCodeKey(id)
	if err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO meetings(id,title,creator,start_time,end_time,tzid,conversation_id) VALUES($1::uuid,'synthetic-cass-code',$1::uuid,'2026-10-09 00:00Z','2026-10-09 00:01Z','UTC',$1::uuid)`, id)
	exec(`INSERT INTO conversation_codes(key,conversation,value,expires_at,target) VALUES($1,$2::uuid,'synthetic',NOW()+INTERVAL '1 hour','meeting')`, key, id)
	result, err = runMeetingsCleanup(ctx, routedMeetingsCleanupStore{Postgres: store, Codes: storageCassandra}, effects, cutoff, 2, nil)
	if err != nil || result.BatchError != nil || result.TotalDeleted != 1 {
		t.Fatal("Cassandra code mode blocked meeting cleanup")
	}
	if conn.QueryRow(ctx, `SELECT count(*) FROM conversation_codes WHERE key=$1`, key).Scan(&count) != nil || count != 1 {
		t.Fatal("Cassandra code mode changed Postgres meeting code")
	}
	t.Log("readonly original meeting/code DDL; two-index merge/cap, full-branch validation, strict expiry/open recurrence, five SQL deletions across batches, code isolation and idempotent rerun pass; conversation effects adapter remains unfinished")
}
