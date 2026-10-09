-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025 Joshua Miller
-- Adapted from Arbiter.Core.Sql.Tree.descendantRollupIdsSQL at b9c57eb1f8277d97616aa449bea471fe9ce14eda.
WITH RECURSIVE descendants AS (
          SELECT id, parent_state FROM "arbiter"."{{queue}}" WHERE parent_id = $1::bigint
          UNION ALL
          SELECT j.id, j.parent_state FROM "arbiter"."{{queue}}" j JOIN descendants d ON j.parent_id = d.id
        )
        SELECT id AS result FROM descendants WHERE parent_state IS NOT NULL
