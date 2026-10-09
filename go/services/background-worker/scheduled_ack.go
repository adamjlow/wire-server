// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	_ "embed"
	"strings"

	"github.com/jackc/pgx/v5"
)

//go:embed arbiter_sql/ack.sql
var scheduledAckTemplate string

//go:embed arbiter_sql/archive_ack.sql
var scheduledArchiveAckTemplate string

func scheduledAckSQL(queue string, archive bool) (string, error) {
	part := ""
	if archive {
		part = scheduledArchiveAckTemplate
	}
	return renderScheduledSQL(strings.ReplaceAll(scheduledAckTemplate, "{{archive}}", part), queue)
}

func (s *postgresStore) AckScheduledJob(ctx context.Context, queue string, job claimedScheduledJob) (affected int64, err error) {
	sql, err := scheduledAckSQL(queue, job.ArchiveFor != nil && *job.ArchiveFor > 0)
	if err != nil {
		return 0, err
	}
	err = retryPostgres(ctx, s.Pool.Reset, func() error {
		conn, err := s.acquire(ctx)
		if err != nil {
			return err
		}
		defer conn.Release()
		tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
		if err != nil {
			return postgresFailure(err, "scheduled acknowledgement transaction failed")
		}
		defer func() { _ = tx.Rollback(ctx) }()
		// Native sibling ACKs serialize on the parent, across implementations.
		// A transaction-scoped lock releases automatically on every exit path.
		if job.ParentID != nil {
			if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text,$2::bigint))`, "arbiter."+queue, *job.ParentID); err != nil {
				return postgresFailure(err, "scheduled parent acknowledgement lock failed")
			}
		}
		if err := tx.QueryRow(ctx, sql, job.ID, job.Attempts).Scan(&affected); err != nil {
			return postgresFailure(err, "scheduled acknowledgement failed")
		}
		if err := tx.Commit(ctx); err != nil {
			return postgresFailure(err, "scheduled acknowledgement commit failed")
		}
		return nil
	})
	return
}
