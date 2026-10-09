-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025 Joshua Miller
-- Adapted from Arbiter.Core.Sql.Workers at b9c57eb1f8277d97616aa449bea471fe9ce14eda.
WITH updated AS (
  UPDATE "arbiter".arbiter_workers SET last_heartbeat=NOW()
  WHERE worker_id=$1::uuid
  RETURNING queue_name,paused
)
SELECT updated.paused OR COALESCE(qm.paused,FALSE)
FROM updated
LEFT JOIN "arbiter".arbiter_queues qm ON qm.queue_name=updated.queue_name
