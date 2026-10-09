-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025 Joshua Miller
-- Adapted from Arbiter.Core.Sql.Groups.lockGroupsSQL at b9c57eb1f8277d97616aa449bea471fe9ce14eda.
SELECT group_key FROM "arbiter"."{{queue}}_groups" FOR UPDATE SKIP LOCKED
