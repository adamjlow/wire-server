-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025 Joshua Miller
-- Adapted from Arbiter.Core.Sql.Gates.ensureGateRowSQL at b9c57eb1f8277d97616aa449bea471fe9ce14eda.
INSERT INTO "arbiter".arbiter_gates (task_name) VALUES ($1::text)
        ON CONFLICT (task_name) DO NOTHING
