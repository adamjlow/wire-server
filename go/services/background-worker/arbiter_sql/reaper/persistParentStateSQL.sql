-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025 Joshua Miller
-- Adapted from Arbiter.Core.Sql.Tree.persistParentStateSQL at b9c57eb1f8277d97616aa449bea471fe9ce14eda.
UPDATE "arbiter"."{{queue}}" SET parent_state = $1::jsonb, updated_at = NOW() WHERE id = $2::bigint
