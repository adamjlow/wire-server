// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

var errScheduledGone = errors.New("scheduled job no longer owned")
var errScheduledForceCancel = errors.New("scheduled job force cancelled")

// Wire registers SingleJobMode handlers returning (), whose EncodeJobResult
// stores nothing. Hold the control transaction across the handler+ACK. Backend
// effects are independent: the native handlers ignore their supplied connection.
// Never replay the handler inside an infrastructure transaction retry.
func (s *postgresStore) CompleteScheduledJob(ctx context.Context, queue string, job claimedScheduledJob, handler func(context.Context) error) error {
	sql, err := scheduledAckSQL(queue, job.ArchiveFor != nil && *job.ArchiveFor > 0)
	if err != nil {
		return err
	}
	conn, err := s.acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return postgresFailure(err, "scheduled handler transaction failed")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := handler(ctx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	affected, err := ackScheduledOnTx(ctx, tx, queue, job, sql)
	if err != nil {
		return err
	}
	if affected == 0 {
		return errScheduledGone
	}
	if err := tx.Commit(ctx); err != nil {
		return postgresFailure(err, "scheduled handler commit failed")
	}
	return nil
}

func (s *postgresStore) FailScheduledJob(ctx context.Context, queue string, job claimedScheduledJob, backoff int64) (changed bool, err error) {
	if _, err := renderScheduledSQL("{{queue}}", queue); err != nil {
		return false, err
	}
	maxAttempts := int32(10)
	if job.MaxAttempts != nil {
		maxAttempts = *job.MaxAttempts
	}
	if job.Attempts < maxAttempts {
		return s.RetryScheduledJob(ctx, queue, job.ID, job.Attempts, backoff)
	}
	// Native moveToDLQ snapshots the current rollup, then descendant rollups and
	// children, moves the exact ID+attempt and resumes its parent on success.
	conn, err := s.acquire(ctx)
	if err != nil {
		return false, err
	}
	defer conn.Release()
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return false, postgresFailure(err, "scheduled failure transaction failed")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if job.ParentState != nil {
		if err := snapshotScheduledRollup(ctx, tx, queue, job.ID); err != nil {
			return false, err
		}
		if err := reaperSnapshotRollups(ctx, tx, queue, job.ID); err != nil {
			return false, err
		}
		if _, err := reaperIDs(ctx, tx, "cascadeChildrenToDLQSQL", queue, job.ID, "Parent moved to DLQ"); err != nil {
			return false, err
		}
	}
	counts, err := reaperIDs(ctx, tx, "moveToDLQSQL", queue, job.ID, job.Attempts, "scheduled job failed")
	if err != nil {
		return false, err
	}
	if len(counts) != 1 {
		return false, errors.New("invalid scheduled failure result")
	}
	changed = counts[0] > 0
	if changed && job.ParentID != nil {
		if err := reaperResumeParent(ctx, tx, queue, *job.ParentID); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return false, postgresFailure(err, "scheduled failure commit failed")
	}
	return changed, nil
}
func (s *postgresStore) DeleteCancelledScheduledJob(ctx context.Context, queue string, id int64) error {
	if _, err := renderScheduledSQL("{{queue}}", queue); err != nil {
		return err
	}
	conn, err := s.acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return postgresFailure(err, "scheduled cancellation transaction failed")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := reaperQuery(ctx, tx, "deleteCancelledJobsSQL", queue, []int64{id})
	if err != nil {
		return err
	}
	var parents []int64
	for rows.Next() {
		var parent *int64
		if err := rows.Scan(&parent); err != nil {
			rows.Close()
			return postgresFailure(err, "scheduled cancellation parent failed")
		}
		if parent != nil {
			parents = append(parents, *parent)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return postgresFailure(err, "scheduled cancellation failed")
	}
	for _, parent := range parents {
		if err := reaperResumeParent(ctx, tx, queue, parent); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return postgresFailure(err, "scheduled cancellation commit failed")
	}
	return nil
}
