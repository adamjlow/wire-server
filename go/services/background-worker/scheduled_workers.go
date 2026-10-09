// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	_ "embed"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

//go:embed arbiter_sql/register_worker.sql
var scheduledRegisterWorkerTemplate string

//go:embed arbiter_sql/heartbeat_worker.sql
var scheduledHeartbeatWorkerTemplate string

func (s *postgresStore) RegisterScheduledWorker(ctx context.Context, queue, id string, host *string, threads int, stale time.Duration) (paused bool, err error) {
	sql, err := renderScheduledSQL(scheduledRegisterWorkerTemplate, queue)
	if err != nil || threads < 1 || threads > 1000 || stale <= 0 {
		return false, errors.New("invalid scheduled worker configuration")
	}
	worker, err := postgresUUID(id)
	if err != nil {
		return false, err
	}
	err = retryPostgres(ctx, s.Pool.Reset, func() error {
		conn, err := s.acquire(ctx)
		if err != nil {
			return err
		}
		defer conn.Release()
		if err := conn.QueryRow(ctx, sql, worker, queue, host, threads, stale.Seconds()).Scan(&paused); err != nil {
			return postgresFailure(err, "scheduled worker registration failed")
		}
		return nil
	})
	return
}

func (s *postgresStore) HeartbeatScheduledWorker(ctx context.Context, id string) (paused, found bool, err error) {
	worker, err := postgresUUID(id)
	if err != nil {
		return false, false, err
	}
	err = retryPostgres(ctx, s.Pool.Reset, func() error {
		conn, err := s.acquire(ctx)
		if err != nil {
			return err
		}
		defer conn.Release()
		err = conn.QueryRow(ctx, scheduledHeartbeatWorkerTemplate, worker).Scan(&paused)
		if errors.Is(err, pgx.ErrNoRows) {
			found = false // The worker loop must re-register after a stale sweep.
			return nil
		}
		if err != nil {
			return postgresFailure(err, "scheduled worker heartbeat failed")
		}
		found = true
		return nil
	})
	return
}

func (s *postgresStore) scheduledWorkerState(ctx context.Context, id string, remove bool) error {
	worker, err := postgresUUID(id)
	if err != nil {
		return err
	}
	sql := `UPDATE "arbiter".arbiter_workers SET shutting_down=TRUE,last_heartbeat=NOW() WHERE worker_id=$1::uuid`
	if remove {
		sql = `DELETE FROM "arbiter".arbiter_workers WHERE worker_id=$1::uuid`
	}
	return retryPostgres(ctx, s.Pool.Reset, func() error {
		conn, err := s.acquire(ctx)
		if err != nil {
			return err
		}
		defer conn.Release()
		if _, err := conn.Exec(ctx, sql, worker); err != nil {
			return postgresFailure(err, "scheduled worker state update failed")
		}
		return nil
	})
}
