// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

//go:embed arbiter_sql/claim.sql
var scheduledClaimTemplate string

type claimedScheduledJob struct {
	ID                int64
	Payload           json.RawMessage
	GroupKey          *string
	Attempts          int32
	MaxAttempts       *int32
	ParentID          *int64
	ParentState       json.RawMessage
	ArchiveFor        *int32
	NotVisibleUntil   *time.Time
	ClaimedBy         pgtype.UUID
	CancelRequestedAt *time.Time
}

func scheduledClaimSQL(queue string) (string, error) {
	return renderScheduledSQL(scheduledClaimTemplate, queue)
}

func renderScheduledSQL(template, queue string) (string, error) {
	switch queue {
	case "meetings", "conversations":
		return strings.ReplaceAll(template, "{{queue}}", queue), nil
	default:
		return "", errors.New("invalid scheduled queue")
	}
}

func (s *postgresStore) ClaimScheduledJobs(ctx context.Context, queue, workerID string, visibility time.Duration, budget int) (jobs []claimedScheduledJob, err error) {
	sql, err := scheduledClaimSQL(queue)
	if err != nil || visibility <= 0 || budget < 1 || budget > 1000 {
		return nil, errors.New("invalid scheduled claim configuration")
	}
	worker, err := postgresUUID(workerID)
	if err != nil {
		return nil, err
	}
	// Claim is one atomic native statement: group rows and job rows use
	// SKIP LOCKED, then attempts/lease/worker ownership change together.
	err = retryPostgres(ctx, s.Pool.Reset, func() error {
		conn, err := s.acquire(ctx)
		if err != nil {
			return err
		}
		defer conn.Release()
		rows, err := conn.Query(ctx, sql, budget, visibility.Seconds(), worker)
		if err != nil {
			return postgresFailure(err, "scheduled job claim failed")
		}
		defer rows.Close()
		attemptJobs := []claimedScheduledJob{}
		for rows.Next() {
			var job claimedScheduledJob
			if err := rows.Scan(&job.ID, &job.Payload, &job.GroupKey, &job.Attempts, &job.MaxAttempts, &job.ParentID, &job.ParentState, &job.ArchiveFor, &job.NotVisibleUntil, &job.ClaimedBy, &job.CancelRequestedAt); err != nil {
				return postgresFailure(err, "scheduled job claim decode failed")
			}
			attemptJobs = append(attemptJobs, job)
		}
		if err := rows.Err(); err != nil {
			return postgresFailure(err, "scheduled job claim failed")
		}
		jobs = attemptJobs
		return nil
	})
	return
}
