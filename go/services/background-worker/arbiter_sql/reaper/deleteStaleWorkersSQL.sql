-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025 Joshua Miller
-- Adapted from Arbiter.Core.Sql.Workers.deleteStaleWorkersSQL at b9c57eb1f8277d97616aa449bea471fe9ce14eda.
DELETE FROM "arbiter".arbiter_workers
        WHERE last_heartbeat < NOW() - (stale_threshold_secs * interval '1 second')
