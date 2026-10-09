-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025 Joshua Miller
-- Adapted from Arbiter.Core.Sql.DLQ.moveToDLQSQL at b9c57eb1f8277d97616aa449bea471fe9ce14eda.
WITH deleted_job AS (
          DELETE FROM "arbiter"."{{queue}}"
          WHERE id = $1::bigint AND attempts = $2::integer
          RETURNING *
        ),
        inserted_dlq AS (
          INSERT INTO "arbiter"."{{queue}}_dlq" (
            job_id, payload,group_key,inserted_at,updated_at,attempts,priority,last_attempted_at,not_visible_until,dedup_key,dedup_strategy,max_attempts,parent_id,parent_state,suspended,claimed_by,archive_for,rate_limit_key,rate_limit_prefix,concurrency_key,concurrency_prefix,rate_limit_cost, last_error
          )
          SELECT
            id, payload,group_key,inserted_at,updated_at,attempts,priority,last_attempted_at,not_visible_until,dedup_key,dedup_strategy,max_attempts,parent_id,parent_state,suspended,claimed_by,archive_for,rate_limit_key,rate_limit_prefix,concurrency_key,concurrency_prefix,rate_limit_cost, $3::text
          FROM deleted_job
        )
        SELECT count(*) AS count FROM deleted_job
