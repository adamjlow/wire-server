-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025 Joshua Miller
-- Adapted from Arbiter.Core.Sql.Tree.tryWakeAncestorSQL at b9c57eb1f8277d97616aa449bea471fe9ce14eda.
UPDATE "arbiter"."{{queue}}"
        SET suspended = FALSE, updated_at = NOW()
        WHERE id = $1::bigint
          AND suspended = TRUE
          AND NOT EXISTS (SELECT 1 FROM "arbiter"."{{queue}}" c WHERE c.parent_id = $1::bigint)
