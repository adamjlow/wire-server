-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025 Joshua Miller
-- Adapted from Arbiter.Core.Sql.Workers at b9c57eb1f8277d97616aa449bea471fe9ce14eda.
WITH upserted AS (
  INSERT INTO "arbiter".arbiter_workers
    (worker_id,queue_name,host_name,worker_count,stale_threshold_secs,metadata,
     started_at,last_heartbeat,shutting_down)
  VALUES ($1::uuid,$2::text,$3::text,$4::int,$5::double precision,NULL,NOW(),NOW(),FALSE)
  ON CONFLICT (worker_id) DO UPDATE
    SET queue_name=EXCLUDED.queue_name,
        host_name=EXCLUDED.host_name,
        worker_count=EXCLUDED.worker_count,
        stale_threshold_secs=EXCLUDED.stale_threshold_secs,
        metadata=EXCLUDED.metadata,
        last_heartbeat=NOW(),
        shutting_down=FALSE
  RETURNING queue_name,paused
)
SELECT u.paused OR COALESCE(qm.paused,FALSE)
FROM upserted u
LEFT JOIN "arbiter".arbiter_queues qm ON qm.queue_name=u.queue_name
