-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025 Joshua Miller
-- Adapted from Arbiter.Core.Sql.Lifecycle at b9c57eb1f8277d97616aa449bea471fe9ce14eda.
-- Single-job form of the native batched heartbeat. ID/attempt/seconds are bound.
WITH input_jobs AS (
  SELECT $1::bigint AS id, $2::int AS expected_attempts
), updated AS (
  UPDATE "arbiter"."{{queue}}" j
  SET not_visible_until = CASE WHEN $3::double precision <= 0 THEN NULL
      ELSE NOW() + ($3::double precision * interval '1 second') END,
      updated_at = NOW()
  FROM input_jobs ij
  WHERE j.id = ij.id AND j.attempts = ij.expected_attempts
  RETURNING j.id
)
SELECT ij.id, (u.id IS NOT NULL), j.attempts,
       (j.cancel_requested_at IS NOT NULL)
FROM input_jobs ij
LEFT JOIN updated u ON ij.id=u.id
LEFT JOIN "arbiter"."{{queue}}" j ON j.id=ij.id
