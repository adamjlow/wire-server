-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025 Joshua Miller
-- Adapted from Arbiter.Core.Sql.Tree.deleteCancelledJobsSQL at b9c57eb1f8277d97616aa449bea471fe9ce14eda.
WITH locked AS (SELECT id FROM "arbiter"."{{queue}}" WHERE id = ANY($1::bigint[]) AND cancel_requested_at IS NOT NULL ORDER BY id DESC FOR UPDATE) DELETE FROM "arbiter"."{{queue}}" WHERE id IN (SELECT id FROM locked) RETURNING parent_id
