-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025 Joshua Miller
-- Adapted from Arbiter.Core.Sql.DLQ.cascadeChildrenToDLQSQL at b9c57eb1f8277d97616aa449bea471fe9ce14eda.
WITH RECURSIVE descendants AS (
          SELECT id FROM "arbiter"."{{queue}}" WHERE parent_id = $1::bigint
          UNION ALL
          SELECT j.id FROM "arbiter"."{{queue}}" j JOIN descendants d ON j.parent_id = d.id
        ),
        deleted AS (
          DELETE FROM "arbiter"."{{queue}}"
          WHERE id IN (SELECT id FROM descendants)
          RETURNING id, payload,group_key,inserted_at,updated_at,attempts,priority,last_attempted_at,not_visible_until,dedup_key,dedup_strategy,max_attempts,parent_id,parent_state,suspended,claimed_by,archive_for,rate_limit_key,rate_limit_prefix,concurrency_key,concurrency_prefix,rate_limit_cost
        ),
        inserted_dlq AS (
          INSERT INTO "arbiter"."{{queue}}_dlq" (job_id, payload,group_key,inserted_at,updated_at,attempts,priority,last_attempted_at,not_visible_until,dedup_key,dedup_strategy,max_attempts,parent_id,parent_state,suspended,claimed_by,archive_for,rate_limit_key,rate_limit_prefix,concurrency_key,concurrency_prefix,rate_limit_cost, last_error)
          SELECT id, payload,group_key,inserted_at,updated_at,attempts,priority,last_attempted_at,not_visible_until,dedup_key,dedup_strategy,max_attempts,parent_id,parent_state,suspended,claimed_by,archive_for,rate_limit_key,rate_limit_prefix,concurrency_key,concurrency_prefix,rate_limit_cost, $2::text
          FROM deleted
        )
        SELECT count(*) AS count FROM deleted
