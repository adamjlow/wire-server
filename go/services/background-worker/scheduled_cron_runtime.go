// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// The worker registers only this cron, with NoBackfill and a parentless job.
// Wire.BackgroundWorker.Workers sets grouped meetings_cleanup, visibility=tick,
// maxAttempts=3. Native defaultJob supplies all other writable values.
const insertMeetingsCronJob = `INSERT INTO arbiter.meetings
 (payload,group_key,attempts,last_error,priority,not_visible_until,dedup_key,dedup_strategy,max_attempts,parent_id,parent_state,suspended,archive_for,rate_limit_key,rate_limit_prefix,rate_limit_cost,concurrency_key,concurrency_prefix)
 VALUES ('{"type":"meetings_cleanup","data":{}}'::jsonb,'meetings-cleanup',0,NULL,0,$1::timestamptz,$2::text,CASE WHEN $2::text IS NULL THEN NULL ELSE 'ignore' END,3,NULL,NULL,false,NULL,NULL,NULL,1,NULL,NULL)
 ON CONFLICT(dedup_key) WHERE dedup_key IS NOT NULL DO NOTHING RETURNING id`

func insertScheduledMeetingCron(ctx context.Context, tx pgx.Tx, tick time.Time, key *string) (bool, error) {
	var id int64
	err := tx.QueryRow(ctx, insertMeetingsCronJob, tick, key).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		if key == nil {
			return false, errors.New("scheduled cron insertion returned no job")
		}
		return false, nil
	}
	if err != nil {
		return false, postgresFailure(err, "scheduled cron insertion failed")
	}
	return true, nil
}
func (s *postgresStore) initializeMeetingsCron(ctx context.Context, expression string) error {
	if _, err := parseScheduledCron(expression); err != nil {
		return err
	}
	return s.upsertScheduledCronDefault(ctx, "meetings-cleanup", "meetings", expression, "SkipOverlap", nil)
}

type cronRunOutcome struct {
	Requested, Inserted bool
	Err                 error
}

func (s *postgresStore) processMeetingsCronManual(ctx context.Context, now time.Time) (out cronRunOutcome) {
	pending, err := s.pendingScheduledCronRuns(ctx, []string{"meetings-cleanup"})
	if err != nil {
		out.Err = err
		return
	}
	if len(pending) == 0 {
		return
	}
	out.Err = retryPostgres(ctx, s.Pool.Reset, func() error {
		out.Requested = false
		out.Inserted = false
		conn, err := s.acquire(ctx)
		if err != nil {
			return err
		}
		defer conn.Release()
		tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
		if err != nil {
			return postgresFailure(err, "scheduled cron manual transaction failed")
		}
		defer func() { _ = tx.Rollback(ctx) }()
		r, err := claimScheduledCronRun(ctx, tx, "meetings-cleanup")
		if err != nil {
			return err
		}
		if r == nil {
			return nil
		}
		out.Requested = true
		var key *string
		if r.effectiveOverlap("SkipOverlap") == "SkipOverlap" {
			key = stringPtr("arbiter_cron:meetings-cleanup")
		}
		// Manual runs deliberately bypass expression/timezone parsing and minute gate.
		inserted, err := insertScheduledMeetingCron(ctx, tx, cronMinute(now), key)
		if err != nil {
			return err
		}
		if inserted {
			if err := touchScheduledCronManual(ctx, tx, cronMinute(now), r.Name); err != nil {
				return err
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return postgresFailure(err, "scheduled cron manual commit failed")
		}
		out.Inserted = inserted
		return nil
	})
	if out.Err != nil {
		out.Inserted = false
	}
	return
}

type cronTickOutcome struct {
	Inserted                      bool
	ConfigError, InsertError, Err error
}

func (s *postgresStore) processMeetingsCronTick(ctx context.Context, expression string, now time.Time) (out cronTickOutcome) {
	tick := cronMinute(now)
	out.Err = retryPostgres(ctx, s.Pool.Reset, func() error {
		out.Inserted = false
		out.ConfigError = nil
		out.InsertError = nil
		conn, err := s.acquire(ctx)
		if err != nil {
			return err
		}
		defer conn.Release()
		tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
		if err != nil {
			return postgresFailure(err, "scheduled cron tick transaction failed")
		}
		defer func() { _ = tx.Rollback(ctx) }()
		r, err := getScheduledCron(ctx, tx, "meetings-cleanup")
		if err != nil {
			return err
		}
		if r == nil {
			r = &scheduledCronRow{Name: "meetings-cleanup", DefaultExpression: expression, DefaultOverlap: "SkipOverlap", Enabled: true}
		}
		if r.Enabled {
			schedule, parseErr := parseScheduledCron(r.effectiveExpression())
			zone, zoneErr := cronTimezone(r.effectiveTimezone())
			// Native parse error wins over timezone error; both are nonfatal and the
			// outer catch-up still records which minute was evaluated.
			if parseErr != nil {
				out.ConfigError = parseErr
			} else if zoneErr != nil {
				out.ConfigError = zoneErr
			} else if (r.LastChecked == nil || cronMinute(*r.LastChecked).Before(tick)) && schedule.matches(tick.In(zone)) {
				// Native tryInsertCronJob uses a nested transaction/savepoint. Failure is
				// caught after rolling back gate+insert, then the outer checked watermark
				// advances. Preserve this distinction, even though a failed minute is lost.
				child, err := tx.Begin(ctx)
				if err != nil {
					return postgresFailure(err, "scheduled cron savepoint failed")
				}
				won, insertErr := fireScheduledCronGate(ctx, child, tick, r.Name)
				inserted := false
				if insertErr == nil && won {
					key := cronDedupKey(r.effectiveOverlap("SkipOverlap"), tick, zone)
					inserted, insertErr = insertScheduledMeetingCron(ctx, child, tick, &key)
				}
				if insertErr == nil {
					insertErr = touchScheduledCronChecked(ctx, child, tick, []string{r.Name})
				}
				if insertErr == nil {
					if err := child.Commit(ctx); err != nil {
						insertErr = postgresFailure(err, "scheduled cron savepoint commit failed")
					}
				}
				if insertErr != nil {
					_ = child.Rollback(ctx)
					out.InsertError = insertErr
				} else {
					out.Inserted = inserted
				}
			}
		}
		if err := touchScheduledCronChecked(ctx, tx, tick, []string{r.Name}); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return postgresFailure(err, "scheduled cron tick commit failed")
		}
		return nil
	})
	if out.Err != nil {
		out.Inserted = false
	}
	return
}

// Runtime will supply the queue-owned NOTIFY wake channel and structured logger.
// Pausing claims does not pause cron in native Arbiter; cancellation stops creation.
type cronSchedulerReport struct {
	Manual *cronRunOutcome
	Tick   *cronTickOutcome
}

func (s *postgresStore) runMeetingsCron(ctx context.Context, expression string, wake <-chan struct{}, report func(cronSchedulerReport)) error {
	if err := s.initializeMeetingsCron(ctx, expression); err != nil {
		return err
	}
	emit := func(r cronSchedulerReport) {
		if report != nil {
			report(r)
		}
	}
	process := func(minute bool) {
		if ctx.Err() != nil {
			return
		}
		now := time.Now()
		manual := s.processMeetingsCronManual(ctx, now)
		emit(cronSchedulerReport{Manual: &manual})
		if minute && ctx.Err() == nil {
			tick := s.processMeetingsCronTick(ctx, expression, now)
			emit(cronSchedulerReport{Tick: &tick})
		}
	}
	process(true)
	for ctx.Err() == nil {
		timer := time.NewTimer(nextCronDelay(time.Now()))
		minute := false
		for !minute && ctx.Err() == nil {
			select {
			case <-ctx.Done():
			case <-timer.C:
				minute = true
			case _, open := <-wake:
				if !open {
					wake = nil
					continue
				}
				// Shutdown has priority. An already-elapsed minute outranks run-now.
				select {
				case <-timer.C:
					minute = true
				default:
				}
				if !minute {
					process(false)
				}
			}
		}
		timer.Stop()
		if minute {
			process(true)
		}
	}
	return nil
}
