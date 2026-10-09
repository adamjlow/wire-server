-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025 Joshua Miller
-- Adapted from Arbiter.Core.Sql.Gates.bumpGateSQL at b9c57eb1f8277d97616aa449bea471fe9ce14eda.
UPDATE "arbiter".arbiter_gates SET last_run_at = NOW() WHERE task_name = $1::text
