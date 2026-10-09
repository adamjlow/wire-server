-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025 Joshua Miller
-- Adapted from Arbiter.Core.Sql.Gates.checkGateSQL at b9c57eb1f8277d97616aa449bea471fe9ce14eda.
SELECT (last_run_at < NOW() - ($2::double precision::double precision * interval '1 second'))
          AS result
        FROM "arbiter".arbiter_gates
        WHERE task_name = $1::text
