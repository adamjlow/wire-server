// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	_ "embed"
	"time"
)

//go:embed arbiter_sql/heartbeat.sql
var scheduledHeartbeatTemplate string

//go:embed arbiter_sql/retry.sql
var scheduledRetryTemplate string

type scheduledHeartbeat struct {
	ID              int64
	Heartbeated     bool
	CurrentAttempts *int32
	CancelRequested bool
}

func (s *postgresStore) HeartbeatScheduledJob(ctx context.Context, queue string, id int64, attempts int32, visibility time.Duration) (result scheduledHeartbeat, err error) {
	sql, err := renderScheduledSQL(scheduledHeartbeatTemplate, queue)
	if err != nil {
		return result, err
	}
	err = retryPostgres(ctx, s.Pool.Reset, func() error {
		conn, err := s.acquire(ctx)
		if err != nil {
			return err
		}
		defer conn.Release()
		if err := conn.QueryRow(ctx, sql, id, attempts, visibility.Seconds()).Scan(&result.ID, &result.Heartbeated, &result.CurrentAttempts, &result.CancelRequested); err != nil {
			return postgresFailure(err, "scheduled job heartbeat failed")
		}
		return nil
	})
	return
}

func (s *postgresStore) RetryScheduledJob(ctx context.Context, queue string, id int64, attempts int32, backoffSeconds int64, diagnostic string) (updated bool, err error) {
	sql, err := renderScheduledSQL(scheduledRetryTemplate, queue)
	if err != nil {
		return false, err
	}
	err = retryPostgres(ctx, s.Pool.Reset, func() error {
		conn, err := s.acquire(ctx)
		if err != nil {
			return err
		}
		defer conn.Release()
		// Store the handler diagnostic, as native classifyException/updateJobForRetry.
		// Production adapters return generic errors; panic values are never formatted.
		result, err := conn.Exec(ctx, sql, id, attempts, backoffSeconds, diagnostic)
		if err != nil {
			return postgresFailure(err, "scheduled job retry update failed")
		}
		updated = result.RowsAffected() > 0
		return nil
	})
	return
}
