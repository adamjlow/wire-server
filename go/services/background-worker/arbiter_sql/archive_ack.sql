-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025 Joshua Miller
-- Adapted from Arbiter.Core.Sql.Archive at b9c57eb1f8277d97616aa449bea471fe9ce14eda.
, archived AS (
  INSERT INTO "arbiter"."{{queue}}_archive"
    (job_id,payload,group_key,inserted_at,updated_at,attempts,last_error,priority,
     last_attempted_at,not_visible_until,dedup_key,dedup_strategy,max_attempts,
     parent_id,parent_state,suspended,claimed_by,archive_for,rate_limit_key,
     rate_limit_prefix,concurrency_key,concurrency_prefix,rate_limit_cost,
     completed_at,archive_expires_at)
  SELECT id,payload,group_key,inserted_at,updated_at,attempts,last_error,priority,
         last_attempted_at,not_visible_until,dedup_key,dedup_strategy,max_attempts,
         parent_id,parent_state,suspended,claimed_by,archive_for,rate_limit_key,
         rate_limit_prefix,concurrency_key,concurrency_prefix,rate_limit_cost,
         NOW(),NOW()+(archive_for * interval '1 second')
  FROM ack WHERE archive_for>0
)
