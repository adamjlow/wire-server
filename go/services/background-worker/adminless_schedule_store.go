// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// Same native groupedJob writable defaults and partial unique dedup index as
// Arbiter.Core.insertJob. Queue and identifiers are fixed; all values bind.
const insertAdminlessScheduledJob = `INSERT INTO arbiter.conversations
 (payload,group_key,attempts,last_error,priority,not_visible_until,dedup_key,dedup_strategy,max_attempts,parent_id,parent_state,suspended,archive_for,rate_limit_key,rate_limit_prefix,rate_limit_cost,concurrency_key,concurrency_prefix)
 VALUES ($1::jsonb,$2::text,0,NULL,0,$3::timestamptz,$4::text,'ignore',3,NULL,NULL,false,NULL,NULL,NULL,1,NULL,NULL)
 ON CONFLICT(dedup_key) WHERE dedup_key IS NOT NULL DO NOTHING RETURNING id`

func (s *postgresStore) InsertAdminlessJob(ctx context.Context, job adminlessScheduledWrite) error {
	conn, err := s.acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	var id int64
	err = conn.QueryRow(ctx, insertAdminlessScheduledJob, job.Payload, job.Group, job.Visible, job.Dedup).Scan(&id)
	if err == nil || errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	return postgresFailure(err, "adminless scheduling failed")
}
