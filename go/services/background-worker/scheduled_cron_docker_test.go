//go:build schemacheck

// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// Fixed owned fixture only, never a user-supplied/shared database target.
func TestScheduledCronDockerStorage(t *testing.T) {
	if os.Getenv("WIRE_SCHEMA_CHECK_SYNTHETIC") != "1" {
		t.Fatal("cron qualifier requires owned synthetic Docker")
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
	name := "meetings-cleanup"
	if err := store.upsertScheduledCronDefault(ctx, name, "meetings", "0 * * * *", "SkipOverlap", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `UPDATE arbiter.cron_schedules SET queue_name='pre-migration',override_expression='',override_overlap='AllowOverlap',override_timezone='UTC',enabled=false WHERE name=$1`, name); err != nil {
		t.Fatal(err)
	}
	if err := store.upsertScheduledCronDefault(ctx, name, "meetings", "30 * * * *", "SkipOverlap", nil); err != nil {
		t.Fatal(err)
	}
	begin := func() pgx.Tx {
		tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
		if err != nil {
			t.Fatal(err)
		}
		return tx
	}
	tx := begin()
	r, err := getScheduledCron(ctx, tx, name)
	if err != nil || r == nil || r.Queue != "meetings" || r.DefaultExpression != "30 * * * *" || r.Enabled || r.effectiveExpression() != "" || r.effectiveOverlap("SkipOverlap") != "AllowOverlap" || r.effectiveTimezone() == nil || *r.effectiveTimezone() != "UTC" {
		t.Fatal("startup overwrote operator state or failed to heal queue")
	}
	_ = tx.Rollback(ctx)
	request := func(n, want string) {
		var got string
		if err := conn.QueryRow(ctx, cronRequestRun, n).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("request status got%s want%s", got, want)
		}
	}
	request("missing", "not_found")
	request(name, "disabled")
	if _, err := conn.Exec(ctx, `UPDATE arbiter.cron_schedules SET enabled=true WHERE name=$1`, name); err != nil {
		t.Fatal(err)
	}
	request(name, "stamped")
	request(name, "pending")
	pending, err := store.pendingScheduledCronRuns(ctx, []string{name, "missing"})
	if err != nil || len(pending) != 1 || pending[0] != name {
		t.Fatal("pending request ownership scan changed")
	}
	// A failed future insertion must roll back the claim, not lose the request.
	tx = begin()
	r, err = claimScheduledCronRun(ctx, tx, name)
	if err != nil || r == nil || r.RunRequested != nil {
		t.Fatal("claim did not atomically clear flag")
	}
	_ = tx.Rollback(ctx)
	pending, err = store.pendingScheduledCronRuns(ctx, []string{name})
	if err != nil || len(pending) != 1 {
		t.Fatal("rolled-back claim lost request")
	}
	tick := time.Date(2026, 10, 9, 12, 34, 0, 0, time.UTC)
	tx = begin()
	r, err = claimScheduledCronRun(ctx, tx, name)
	if err != nil || r == nil {
		t.Fatal("request no longer claimable")
	}
	if err := touchScheduledCronManual(ctx, tx, tick, name); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	pending, err = store.pendingScheduledCronRuns(ctx, []string{name})
	if err != nil || len(pending) != 0 {
		t.Fatal("completed claim remained pending")
	}
	tx = begin()
	r, err = claimScheduledCronRun(ctx, tx, name)
	if err != nil || r != nil {
		t.Fatal("request claimed twice")
	}
	_ = tx.Rollback(ctx)
	if _, err := conn.Exec(ctx, `UPDATE arbiter.cron_schedules SET run_requested_at=NOW()-INTERVAL '6 minutes' WHERE name=$1`, name); err != nil {
		t.Fatal(err)
	}
	tx = begin()
	r, err = getScheduledCron(ctx, tx, name)
	if err != nil || r == nil || r.RunRequested != nil {
		t.Fatal("expired request visible to scheduler")
	}
	r, err = claimScheduledCronRun(ctx, tx, name)
	if err != nil || r != nil {
		t.Fatal("expired request claimable")
	}
	_ = tx.Rollback(ctx)
	request(name, "stamped") // An expired physical flag must not block fresh requests.
	tx = begin()
	won, err := fireScheduledCronGate(ctx, tx, tick, name)
	if err != nil || !won {
		t.Fatal("initial minute gate failed")
	}
	if err := touchScheduledCronChecked(ctx, tx, tick, []string{name}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	tx = begin()
	won, err = fireScheduledCronGate(ctx, tx, tick, name)
	if err != nil || won {
		t.Fatal("minute fired twice")
	}
	won, err = fireScheduledCronGate(ctx, tx, tick.Add(-time.Minute), name)
	if err != nil || won {
		t.Fatal("older minute fired")
	}
	if err := touchScheduledCronChecked(ctx, tx, tick.Add(-time.Minute), []string{name}); err != nil {
		t.Fatal(err)
	}
	if err := touchScheduledCronManual(ctx, tx, tick.Add(-time.Minute), name); err != nil {
		t.Fatal(err)
	}
	r, err = getScheduledCron(ctx, tx, name)
	if err != nil || r == nil || !r.LastChecked.Equal(tick) || !r.LastManualRun.Equal(tick) {
		t.Fatal("watermark moved backwards")
	}
	_ = tx.Rollback(ctx)
	// Gate and watermark rollback together when future job insertion fails.
	tx = begin()
	won, err = fireScheduledCronGate(ctx, tx, tick.Add(time.Minute), name)
	if err != nil || !won {
		t.Fatal("next minute gate failed")
	}
	_ = tx.Rollback(ctx)
	tx = begin()
	won, err = fireScheduledCronGate(ctx, tx, tick.Add(time.Minute), name)
	if err != nil || !won {
		t.Fatal("failed insertion consumed minute")
	}
	_ = tx.Rollback(ctx)
	// Native backfill leadership is nonblocking and transaction scoped.
	tx = begin()
	won, err = acquireScheduledCronLeader(ctx, tx, "meetings", name)
	if err != nil || !won {
		t.Fatal("leader acquisition failed")
	}
	other, err := store.acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Release()
	rival, err := other.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	won, err = acquireScheduledCronLeader(ctx, rival, "meetings", name)
	if err != nil || won {
		t.Fatal("leader excluded no rival")
	}
	_ = tx.Rollback(ctx)
	won, err = acquireScheduledCronLeader(ctx, rival, "meetings", name)
	if err != nil || !won {
		t.Fatal("leader lock survived transaction")
	}
	_ = rival.Rollback(ctx)
	t.Log("operator overrides, queue healing, request statuses/expiry/rollback, monotone minute/manual watermarks and transaction-scoped leadership pass")
}
