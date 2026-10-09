-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025 Joshua Miller
-- Adapted from Arbiter.Core.Sql.Tree.selectCancelledReapableJobsSQL at b9c57eb1f8277d97616aa449bea471fe9ce14eda.
SELECT id
        FROM "arbiter"."{{queue}}"
        WHERE cancel_requested_at IS NOT NULL
          AND (not_visible_until IS NULL OR not_visible_until <= NOW())
        ORDER BY id ASC
        LIMIT 1000
