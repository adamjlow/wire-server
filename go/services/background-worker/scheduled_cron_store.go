// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// SQL and row ordering follow Arbiter Core.Sql.Cron and Core.Codec at
// b9c57eb1f8277d97616aa449bea471fe9ce14eda. Adapted portions are MIT licensed;
// see arbiter_sql/LICENSE. Identifiers are fixed; all values remain bound.
const cronColumns = `name,queue_name,default_expression,default_overlap,default_timezone,override_expression,override_overlap,override_timezone,enabled,last_fired_at,last_checked_at,run_requested_at,last_manual_run_at,created_at,updated_at`
const cronPending = `(run_requested_at IS NOT NULL AND run_requested_at > NOW()-INTERVAL '5 minutes')`
const cronReadColumns = `name,queue_name,default_expression,default_overlap,default_timezone,override_expression,override_overlap,override_timezone,enabled,last_fired_at,last_checked_at,CASE WHEN ` + cronPending + ` THEN run_requested_at END AS run_requested_at,last_manual_run_at,created_at,updated_at`
const cronUpsertDefault = `INSERT INTO arbiter.cron_schedules(name,queue_name,default_expression,default_overlap,default_timezone)
 VALUES($1::text,$2::text,$3::text,$4::text,$5::text)
 ON CONFLICT(name) DO UPDATE SET queue_name=EXCLUDED.queue_name,default_expression=EXCLUDED.default_expression,default_overlap=EXCLUDED.default_overlap,default_timezone=EXCLUDED.default_timezone,updated_at=NOW()`
const cronRequestRun = `WITH found AS (SELECT enabled,run_requested_at FROM arbiter.cron_schedules WHERE name=$1::text FOR UPDATE),
 upd AS (UPDATE arbiter.cron_schedules SET run_requested_at=NOW(),updated_at=NOW()
 WHERE name=$1::text AND enabled AND NOT ` + cronPending + ` RETURNING pg_notify('arbiter_cron_run_arbiter',name))
 SELECT CASE WHEN EXISTS(SELECT 1 FROM upd) THEN 'stamped'
 WHEN EXISTS(SELECT 1 FROM found WHERE enabled AND ` + cronPending + `) THEN 'pending'
 WHEN EXISTS(SELECT 1 FROM found) THEN 'disabled' ELSE 'not_found' END`
const cronClaimRun = `UPDATE arbiter.cron_schedules SET run_requested_at=NULL,updated_at=NOW()
 WHERE name=$1::text AND enabled AND ` + cronPending + ` RETURNING ` + cronColumns
const cronFireGate = `UPDATE arbiter.cron_schedules SET last_fired_at=GREATEST(last_fired_at,$1::timestamptz)
 WHERE name=$2::text AND (last_fired_at IS NULL OR last_fired_at<$1::timestamptz)`
const cronTouchChecked = `UPDATE arbiter.cron_schedules SET last_checked_at=GREATEST(last_checked_at,$1::timestamptz) WHERE name=ANY($2::text[])`
const cronTouchManual = `UPDATE arbiter.cron_schedules SET last_manual_run_at=GREATEST(last_manual_run_at,$1::timestamptz),updated_at=NOW() WHERE name=$2::text`
const cronAcquireLeader = `SELECT pg_try_advisory_xact_lock(hashtextextended($1::text||':'||$2::text||':'||$3::text,0))`
const cronPendingRuns = `SELECT name FROM arbiter.cron_schedules WHERE name=ANY($1::text[]) AND enabled AND ` + cronPending

// Pointer fields preserve NULL versus empty overrides and absent watermarks.
type scheduledCronRow struct {
	Name, Queue, DefaultExpression, DefaultOverlap                         string
	DefaultTimezone, OverrideExpression, OverrideOverlap, OverrideTimezone *string
	Enabled                                                                bool
	LastFired, LastChecked, RunRequested, LastManualRun                    *time.Time
	Created, Updated                                                       time.Time
}

func scanScheduledCron(row pgx.Row) (*scheduledCronRow, error) {
	var r scheduledCronRow
	err := row.Scan(&r.Name, &r.Queue, &r.DefaultExpression, &r.DefaultOverlap, &r.DefaultTimezone, &r.OverrideExpression, &r.OverrideOverlap, &r.OverrideTimezone, &r.Enabled, &r.LastFired, &r.LastChecked, &r.RunRequested, &r.LastManualRun, &r.Created, &r.Updated)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, postgresFailure(err, "scheduled cron row read failed")
	}
	return &r, nil
}

func (r scheduledCronRow) effectiveExpression() string {
	if r.OverrideExpression != nil {
		return *r.OverrideExpression
	}
	return r.DefaultExpression
}
func (r scheduledCronRow) effectiveTimezone() *string {
	if r.OverrideTimezone != nil {
		return r.OverrideTimezone
	}
	return r.DefaultTimezone
}

// Native fallback is the code-defined policy, not a malformed stored value.
func (r scheduledCronRow) effectiveOverlap(codeDefault string) string {
	v := r.DefaultOverlap
	if r.OverrideOverlap != nil {
		v = *r.OverrideOverlap
	}
	if v == "SkipOverlap" || v == "AllowOverlap" {
		return v
	}
	return codeDefault
}

func getScheduledCron(ctx context.Context, tx pgx.Tx, name string) (*scheduledCronRow, error) {
	return scanScheduledCron(tx.QueryRow(ctx, `SELECT `+cronReadColumns+` FROM arbiter.cron_schedules WHERE name=$1::text`, name))
}
func claimScheduledCronRun(ctx context.Context, tx pgx.Tx, name string) (*scheduledCronRow, error) {
	// Use the caller's transaction so clearing the request and inserting its job
	// succeed together; an insert failure must leave the request claimable.
	return scanScheduledCron(tx.QueryRow(ctx, cronClaimRun, name))
}
func fireScheduledCronGate(ctx context.Context, tx pgx.Tx, tick time.Time, name string) (bool, error) {
	tag, err := tx.Exec(ctx, cronFireGate, tick, name)
	if err != nil {
		return false, postgresFailure(err, "scheduled cron fire gate failed")
	}
	return tag.RowsAffected() > 0, nil
}
func touchScheduledCronChecked(ctx context.Context, tx pgx.Tx, tick time.Time, names []string) error {
	_, err := tx.Exec(ctx, cronTouchChecked, tick, names)
	if err != nil {
		return postgresFailure(err, "scheduled cron watermark failed")
	}
	return nil
}
func touchScheduledCronManual(ctx context.Context, tx pgx.Tx, tick time.Time, name string) error {
	_, err := tx.Exec(ctx, cronTouchManual, tick, name)
	if err != nil {
		return postgresFailure(err, "scheduled cron manual watermark failed")
	}
	return nil
}
func acquireScheduledCronLeader(ctx context.Context, tx pgx.Tx, queue, name string) (bool, error) {
	if _, err := renderScheduledSQL("{{queue}}", queue); err != nil {
		return false, err
	}
	var won bool
	if err := tx.QueryRow(ctx, cronAcquireLeader, "arbiter", queue, name).Scan(&won); err != nil {
		return false, postgresFailure(err, "scheduled cron leader claim failed")
	}
	return won, nil
}

func (s *postgresStore) upsertScheduledCronDefault(ctx context.Context, name, queue, expression, overlap string, zone *string) error {
	if _, err := renderScheduledSQL("{{queue}}", queue); err != nil {
		return err
	}
	if overlap != "SkipOverlap" && overlap != "AllowOverlap" {
		return errors.New("invalid scheduled cron overlap")
	}
	return retryPostgres(ctx, s.Pool.Reset, func() error {
		conn, err := s.acquire(ctx)
		if err != nil {
			return err
		}
		defer conn.Release()
		if _, err := conn.Exec(ctx, cronUpsertDefault, name, queue, expression, overlap, zone); err != nil {
			return postgresFailure(err, "scheduled cron initialization failed")
		}
		return nil
	})
}
func (s *postgresStore) pendingScheduledCronRuns(ctx context.Context, names []string) (pending []string, err error) {
	err = retryPostgres(ctx, s.Pool.Reset, func() error {
		pending = nil
		conn, err := s.acquire(ctx)
		if err != nil {
			return err
		}
		defer conn.Release()
		rows, err := conn.Query(ctx, cronPendingRuns, names)
		if err != nil {
			return postgresFailure(err, "scheduled cron request scan failed")
		}
		defer rows.Close()
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				return postgresFailure(err, "scheduled cron request decode failed")
			}
			pending = append(pending, name)
		}
		if err := rows.Err(); err != nil {
			return postgresFailure(err, "scheduled cron request scan failed")
		}
		return nil
	})
	return
}
