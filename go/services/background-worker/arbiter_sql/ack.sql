-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025 Joshua Miller
-- Adapted from Arbiter.Core.Sql.Lifecycle at b9c57eb1f8277d97616aa449bea471fe9ce14eda.
WITH ack AS (
  DELETE FROM "arbiter"."{{queue}}"
  WHERE id=$1::bigint AND attempts=$2::int
    AND NOT EXISTS (SELECT 1 FROM "arbiter"."{{queue}}" WHERE parent_id=$1::bigint)
  RETURNING *
){{archive}},
suspend AS (
  UPDATE "arbiter"."{{queue}}"
  SET suspended=TRUE,not_visible_until=NULL,claimed_by=NULL,updated_at=NOW()
  WHERE id=$1::bigint AND attempts=$2::int
    AND NOT EXISTS (SELECT 1 FROM ack)
    AND EXISTS (SELECT 1 FROM "arbiter"."{{queue}}" WHERE parent_id=$1::bigint)
  RETURNING id
),
wake_parent AS (
  UPDATE "arbiter"."{{queue}}"
  SET suspended=FALSE,updated_at=NOW()
  WHERE id=(SELECT parent_id FROM ack WHERE parent_id IS NOT NULL)
    AND suspended=TRUE
    AND NOT EXISTS (
      SELECT 1 FROM "arbiter"."{{queue}}" c
      WHERE c.parent_id=(SELECT parent_id FROM ack WHERE parent_id IS NOT NULL)
        AND c.id NOT IN (SELECT id FROM ack)
    )
  RETURNING id
)
SELECT (SELECT count(*) FROM ack)+(SELECT count(*) FROM suspend)
