-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025 Joshua Miller
-- Adapted from velveteer/arbiter b9c57eb1f8277d97616aa449bea471fe9ce14eda
-- Arbiter.Core.Sql.Claim, no admission policies and single-job batches.
-- Arguments: batch budget, visibility seconds, worker UUID. Queue is whitelisted.
WITH
group_candidates AS (
  (
    SELECT group_key FROM "arbiter"."{{queue}}_groups"
    WHERE ready_count > 0 AND in_flight_until IS NULL
    ORDER BY min_priority ASC, min_id ASC
    LIMIT ($1::int * 10)
  )
  UNION
  (
    SELECT group_key FROM "arbiter"."{{queue}}_groups"
    WHERE next_due <= NOW()
    ORDER BY next_due ASC
    LIMIT ($1::int * 10)
  )
),
eligible_groups AS (
  SELECT g.group_key FROM "arbiter"."{{queue}}_groups" g
  WHERE g.group_key IN (SELECT group_key FROM group_candidates)
    AND g.job_count > 0
    AND (g.in_flight_until IS NULL OR g.in_flight_until <= NOW())
  ORDER BY g.min_priority ASC, g.min_id ASC
  LIMIT ($1::int * 10)
  FOR UPDATE SKIP LOCKED
),
eligible_heads AS (
  SELECT el.group_key, h.min_priority, h.min_id
  FROM eligible_groups el
  CROSS JOIN LATERAL (
    SELECT t.priority AS min_priority, t.id AS min_id
    FROM "arbiter"."{{queue}}" t
    WHERE t.group_key = el.group_key
      AND NOT t.suspended
      AND t.cancel_requested_at IS NULL
      AND (t.not_visible_until IS NULL OR t.not_visible_until <= NOW())
      AND t.attempts < COALESCE(t.max_attempts, 10)
    ORDER BY t.priority ASC, t.id ASC
    LIMIT 1
  ) h
),
ungrouped_pool AS (
  (
    SELECT j.id, j.priority
    FROM "arbiter"."{{queue}}" j
    WHERE j.group_key IS NULL
      AND NOT j.suspended
      AND j.cancel_requested_at IS NULL
      AND j.not_visible_until IS NULL
      AND j.attempts < COALESCE(j.max_attempts, 10)
      
    ORDER BY j.priority ASC, j.id ASC
    LIMIT $1::int
  )
  UNION ALL
  (
    SELECT j.id, j.priority
    FROM "arbiter"."{{queue}}" j
    WHERE j.group_key IS NULL
      AND NOT j.suspended
      AND j.cancel_requested_at IS NULL
      AND j.not_visible_until IS NOT NULL
      AND j.not_visible_until <= NOW()
      AND j.attempts < COALESCE(j.max_attempts, 10)
      
    ORDER BY j.not_visible_until ASC
    LIMIT $1::int
  )
),
ungrouped_numbered AS (
  SELECT id, priority,
    ((ROW_NUMBER() OVER (ORDER BY priority ASC, id ASC) - 1)
      / 1) + 1 AS batch_num
  FROM ungrouped_pool
  ORDER BY priority ASC, id ASC
  LIMIT $1::int
),
ungrouped_batch_info AS (
  SELECT batch_num, MIN(priority) AS min_priority, MIN(id) AS min_id
  FROM ungrouped_numbered
  GROUP BY batch_num
),
allocated_slots AS (
  SELECT s.group_key, s.ungrouped_batch
  FROM (
    SELECT group_key, NULL::bigint AS ungrouped_batch, min_priority, min_id
    FROM eligible_heads
    UNION ALL
    SELECT NULL::text, batch_num, min_priority, min_id
    FROM ungrouped_batch_info
    ORDER BY min_priority ASC, min_id ASC
  ) s
  LIMIT $1::int
),
final_locked_groups AS (
  SELECT group_key FROM allocated_slots WHERE group_key IS NOT NULL
),
grouped_candidates AS (
  SELECT j.id, flg.group_key AS expected_group
  FROM final_locked_groups flg
  CROSS JOIN LATERAL (
    SELECT id
    FROM "arbiter"."{{queue}}"
    WHERE group_key = flg.group_key
      AND NOT suspended
      AND cancel_requested_at IS NULL
      AND (not_visible_until IS NULL OR not_visible_until <= NOW())
      AND attempts < COALESCE(max_attempts, 10)
    ORDER BY attempts DESC, priority ASC, id ASC
    LIMIT 1
  ) j
),
ungrouped_candidates AS (
  SELECT id, NULL::text AS expected_group
  FROM ungrouped_numbered
  WHERE batch_num IN (
    SELECT ungrouped_batch
    FROM allocated_slots
    WHERE ungrouped_batch IS NOT NULL
  )
),
locked AS (
  SELECT j.id
  FROM (
    SELECT id, expected_group FROM grouped_candidates
    UNION ALL
    SELECT id, expected_group FROM ungrouped_candidates
  ) i
  INNER JOIN "arbiter"."{{queue}}" j ON j.id = i.id
  WHERE NOT j.suspended
    AND j.cancel_requested_at IS NULL
    AND (j.not_visible_until IS NULL OR j.not_visible_until <= NOW())
    AND j.group_key IS NOT DISTINCT FROM i.expected_group
    AND j.attempts < COALESCE(j.max_attempts, 10)
  ORDER BY j.priority ASC, j.id ASC
  FOR UPDATE OF j SKIP LOCKED
  LIMIT $1::int
),
admitted AS (SELECT id FROM locked),
claimed AS (
  UPDATE "arbiter"."{{queue}}" j
  SET not_visible_until = NOW() + ($2::double precision * interval '1 second'),
      attempts = j.attempts + 1,
      last_attempted_at = NOW(),
      updated_at = NOW(),
      claimed_by = $3::uuid
  FROM admitted a
  WHERE j.id = a.id
  RETURNING j.*
)
SELECT id,payload,group_key,attempts,max_attempts,parent_id,parent_state,archive_for,not_visible_until,claimed_by,cancel_requested_at FROM claimed ORDER BY priority ASC,id ASC
