//go:build schemacheck

// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestAdminlessSchedulingDockerStorage(t *testing.T) {
	if os.Getenv("WIRE_SCHEMA_CHECK_SYNTHETIC") != "1" {
		t.Fatal("requires owned synthetic Docker fixture")
	}
	ctx, stop := context.WithTimeout(t.Context(), time.Minute)
	defer stop()
	galley := GalleyConfig{Postgres: map[string]string{"host": "postgres", "port": "5432", "user": "postgres", "password": "synthetic-schema-only", "dbname": "backendA", "sslmode": "disable"}}
	config := PostgresPoolConfig{Size: 2, Acquisition: Duration(5 * time.Second), Idleness: Duration(time.Minute)}
	if err := runScheduledSchema(ctx, config, galley); err != nil {
		t.Fatal(err)
	}
	store, err := openPostgres(ctx, config, galley)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Pool.Close()
	now := time.Date(2026, 10, 9, 10, 11, 12, 123456789, time.UTC)
	scheduler := adminlessDurableScheduler{Store: store, Now: func() time.Time { return now }}
	feature := defaultAdminlessFeature().Feature
	conv := &storedConversation{ID: jobUUID}
	if err := scheduler.Schedule(ctx, jobUUID, conv, nil, "synthetic-original", feature); err != nil {
		t.Fatal(err)
	}
	// Retry with changed request/clock must preserve original dedup rows, payload,
	// timing and writable defaults. Native triggers must produce serial groups.
	scheduler.Now = func() time.Time { return now.Add(time.Hour) }
	if err := scheduler.Schedule(ctx, jobUUID, conv, stringPtr(jobUUID), "synthetic-retry", feature); err != nil {
		t.Fatal(err)
	}
	rows, err := store.Pool.Query(ctx, `SELECT payload,group_key,dedup_key,not_visible_until,attempts,priority,max_attempts,dedup_strategy,suspended,rate_limit_cost,(parent_id IS NULL AND parent_state IS NULL AND archive_for IS NULL AND rate_limit_key IS NULL AND concurrency_key IS NULL AND concurrency_prefix IS NULL) FROM arbiter.conversations ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for rows.Next() {
		var payload []byte
		var group, key, strategy string
		var visible time.Time
		var attempts, priority, maxAttempts, cost int
		var suspended, nullDefaults bool
		if err := rows.Scan(&payload, &group, &key, &visible, &attempts, &priority, &maxAttempts, &strategy, &suspended, &cost, &nullDefaults); err != nil {
			t.Fatal(err)
		}
		job, err := decodeScheduledPayload("conversations", payload)
		if err != nil || job.RequestID != "synthetic-original" || job.Actor != nil || attempts != 0 || priority != 0 || maxAttempts != 3 || strategy != "ignore" || suspended || cost != 1 || !nullDefaults {
			t.Fatal("dedup replacement/payload/default mismatch")
		}
		if count == 0 {
			if job.Type != "adminless_deletion" || group != "adminless-deletion:"+jobUUID || key != group || !visible.Equal(now.Add(7*24*time.Hour).Truncate(time.Microsecond)) {
				t.Fatal("deletion scheduling mismatch")
			}
		} else {
			offsets := []time.Duration{5 * 24 * time.Hour, 3 * 24 * time.Hour, 24 * time.Hour}
			dedups := []string{"172800s", "345600s", "518400s"}
			if count > 3 || job.Type != "adminless_reminder" || group != "adminless-reminder:"+jobUUID || key != group+":"+dedups[count-1] || !visible.Equal(now.Add(offsets[count-1]).Truncate(time.Microsecond)) || job.DeletionScheduledFor == nil || !job.DeletionScheduledFor.Equal(now.Add(7*24*time.Hour).Truncate(time.Millisecond)) {
				t.Fatal("reminder scheduling mismatch")
			}
		}
		count++
	}
	err = rows.Err()
	rows.Close()
	if err != nil || count != 4 {
		t.Fatal("missing jobs", err, count)
	}
	var groups int
	if store.Pool.QueryRow(ctx, `SELECT count(*) FROM arbiter.conversations_groups`).Scan(&groups) != nil || groups != 2 {
		t.Fatal("native group triggers mismatch")
	}
	// A JSON NUL fails PostgreSQL jsonb parsing. No sensitive diagnostics escape;
	// previously durable rows survive and the scheduler stops at the failed insert.
	if err := scheduler.Schedule(ctx, jobUUID, &storedConversation{ID: "00000000-0000-0000-0000-000000000002"}, nil, "\x00", feature); err == nil {
		t.Fatal("invalid storage payload accepted")
	}
	var durable int
	if store.Pool.QueryRow(ctx, `SELECT count(*) FROM arbiter.conversations`).Scan(&durable) != nil || durable != 4 {
		t.Fatal("prior durable rows lost")
	}
}
