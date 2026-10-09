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

func TestScheduledCronDockerInsertion(t *testing.T) {
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
	exec := func(sql string, args ...any) {
		if _, err := conn.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`DELETE FROM arbiter.meetings; DELETE FROM arbiter.cron_schedules`)
	expression := "* * * * *"
	name := "meetings-cleanup"
	if err := store.initializeMeetingsCron(ctx, expression); err != nil {
		t.Fatal(err)
	}
	tick := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	fire := func(at time.Time, want bool) {
		o := store.processMeetingsCronTick(ctx, expression, at)
		if o.Err != nil || o.ConfigError != nil || o.InsertError != nil || o.Inserted != want {
			t.Fatalf("tick outcome %+v want inserted%v", o, want)
		}
	}
	count := func(want int) {
		var n int
		if conn.QueryRow(ctx, `SELECT count(*) FROM arbiter.meetings`).Scan(&n) != nil || n != want {
			t.Fatalf("job count%d want%d", n, want)
		}
	}
	request := func() {
		var status string
		if conn.QueryRow(ctx, cronRequestRun, name).Scan(&status) != nil || status != "stamped" {
			t.Fatal("manual request not stamped")
		}
	}
	manual := func(want bool) {
		o := store.processMeetingsCronManual(ctx, tick)
		if o.Err != nil || !o.Requested || o.Inserted != want {
			t.Fatalf("manual outcome %+v want inserted%v", o, want)
		}
	}
	fire(tick, true)
	fire(tick, false)
	fire(tick.Add(time.Minute), false)
	count(1)
	var valid bool
	if err := conn.QueryRow(ctx, `SELECT payload='{"type":"meetings_cleanup","data":{}}'::jsonb AND group_key='meetings-cleanup' AND priority=0 AND attempts=0 AND max_attempts=3 AND not_visible_until=$1 AND dedup_key='arbiter_cron:meetings-cleanup' AND dedup_strategy='ignore' AND NOT suspended AND parent_id IS NULL AND archive_for IS NULL AND claimed_by IS NULL AND rate_limit_key IS NULL AND concurrency_key IS NULL AND rate_limit_cost=1 FROM arbiter.meetings`, tick).Scan(&valid); err != nil || !valid {
		t.Fatal("native defaultGroupedJob fields differ")
	}
	request()
	manual(false)
	count(1)
	var completed *time.Time
	if conn.QueryRow(ctx, `SELECT last_manual_run_at FROM arbiter.cron_schedules WHERE name=$1`, name).Scan(&completed) != nil || completed != nil {
		t.Fatal("dedup-skipped manual run recorded a fire")
	}
	exec(`DELETE FROM arbiter.meetings`)
	fire(tick.Add(2*time.Minute), true)
	// Manual AllowOverlap intentionally bypasses invalid effective expression/TZ
	// and creates jobs with no dedup key while preserving the serial group key.
	exec(`UPDATE arbiter.cron_schedules SET override_expression='invalid',override_timezone='invalid/synthetic',override_overlap='AllowOverlap' WHERE name=$1`, name)
	request()
	manual(true)
	request()
	manual(true)
	count(3)
	var n int
	if conn.QueryRow(ctx, `SELECT count(*) FROM arbiter.meetings WHERE dedup_key IS NULL AND dedup_strategy IS NULL AND group_key='meetings-cleanup'`).Scan(&n) != nil || n != 2 {
		t.Fatal("manual overlap jobs incorrectly deduped")
	}
	var groupCount int
	if conn.QueryRow(ctx, `SELECT job_count FROM arbiter.meetings_groups WHERE group_key='meetings-cleanup'`).Scan(&groupCount) != nil || groupCount != 3 {
		t.Fatal("native insert trigger did not maintain serial group")
	}
	bad := store.processMeetingsCronTick(ctx, expression, tick.Add(3*time.Minute))
	if bad.Err != nil || bad.ConfigError == nil || bad.Inserted {
		t.Fatal("invalid schedule did not report and skip")
	}
	// DST fold: distinct UTC fire gates share a local-minute dedup key.
	exec(`DELETE FROM arbiter.meetings; UPDATE arbiter.cron_schedules SET override_expression='30 1 * * *',override_timezone='America/New_York'`)
	fold := time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC)
	fire(fold, true)
	fire(fold.Add(time.Hour), false)
	count(1)
	var key string
	if conn.QueryRow(ctx, `SELECT dedup_key FROM arbiter.meetings`).Scan(&key) != nil || key != "arbiter_cron:meetings-cleanup:2026-11-01T01:30" {
		t.Fatal("DST dedup key differs")
	}
	exec(`UPDATE arbiter.cron_schedules SET enabled=false WHERE name=$1`, name)
	fire(fold.Add(2*time.Hour), false)
	count(1)
	// Real insertion failure: manual claim rolls back; scheduled inner gate
	// rolls back but native outer catch-up still records the evaluated minute.
	exec(`DELETE FROM arbiter.meetings; UPDATE arbiter.cron_schedules SET enabled=true,override_expression='* * * * *',override_timezone='UTC',override_overlap='SkipOverlap'; ALTER TABLE arbiter.meetings ADD CONSTRAINT synthetic_cron_fault CHECK(payload->>'type'<>'meetings_cleanup') NOT VALID`)
	request()
	failedManual := store.processMeetingsCronManual(ctx, fold.Add(3*time.Hour))
	if failedManual.Err == nil || failedManual.Inserted {
		t.Fatal("failed insertion did not fail manual claim")
	}
	pending, err := store.pendingScheduledCronRuns(ctx, []string{name})
	if err != nil || len(pending) != 1 {
		t.Fatal("failed insertion lost manual request")
	}
	failTick := fold.Add(3 * time.Hour)
	failedTick := store.processMeetingsCronTick(ctx, expression, failTick)
	if failedTick.Err != nil || failedTick.InsertError == nil || failedTick.Inserted {
		t.Fatal("native caught insert failure distinction lost")
	}
	var checked, fired time.Time
	if conn.QueryRow(ctx, `SELECT last_checked_at,last_fired_at FROM arbiter.cron_schedules WHERE name=$1`, name).Scan(&checked, &fired) != nil || !checked.Equal(failTick) || !fired.Equal(fold.Add(time.Hour)) {
		t.Fatal("native nested failure watermarks differ")
	}
	exec(`ALTER TABLE arbiter.meetings DROP CONSTRAINT synthetic_cron_fault`)
	fire(failTick, false)
	count(0) // Native NoBackfill does not replay this lost tick.
	o := store.processMeetingsCronManual(ctx, failTick)
	if o.Err != nil || !o.Requested || !o.Inserted {
		t.Fatal("preserved manual request did not resume")
	}
	count(1)

	// Exercise startup/manual wake without waiting a minute. Future checked state
	// suppresses automatic firing in this synthetic fixture.
	exec(`DELETE FROM arbiter.meetings`)
	request()
	loopCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	wake := make(chan struct{}, 1)
	reports := make(chan cronSchedulerReport, 8)
	stopped := make(chan error, 1)
	go func() {
		stopped <- store.runMeetingsCron(loopCtx, expression, wake, func(r cronSchedulerReport) {
			select {
			case reports <- r:
			case <-loopCtx.Done():
			}
		})
	}()
	receive := func() cronSchedulerReport {
		select {
		case r := <-reports:
			return r
		case <-time.After(5 * time.Second):
			t.Fatal("scheduler outcome expired")
			return cronSchedulerReport{}
		}
	}
	first := receive()
	if first.Manual == nil || first.Manual.Err != nil || !first.Manual.Inserted {
		t.Fatal("startup did not serve pending manual request")
	}
	second := receive()
	if second.Tick == nil || second.Tick.Err != nil || second.Tick.Inserted {
		t.Fatal("startup ignored checked watermark")
	}
	exec(`DELETE FROM arbiter.meetings`)
	request()
	wake <- struct{}{}
	third := receive()
	if third.Manual == nil || third.Manual.Err != nil || !third.Manual.Inserted {
		t.Fatal("run-now wake did not serve request")
	}
	cancel()
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("scheduler did not stop on cancellation")
	}
	count(1)
	t.Log("native job fields/group triggers, minute/overlap/manual rules, DST dedup, disabled/invalid config, real insertion failures, startup/run-now and bounded cancellation pass")
}
