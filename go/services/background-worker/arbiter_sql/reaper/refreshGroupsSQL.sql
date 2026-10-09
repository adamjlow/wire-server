-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025 Joshua Miller
-- Adapted from Arbiter.Core.Sql.Groups.refreshGroupsSQL at b9c57eb1f8277d97616aa449bea471fe9ce14eda.
WITH params AS (
          SELECT unnest($1::text[]::text[]) AS group_key
        ),
        current AS (
          SELECT group_key,
                 MIN(priority) AS min_priority,
                 MIN(id) AS min_id,
                 COUNT(*) AS job_count,
                 COUNT(*) FILTER (WHERE not_visible_until IS NULL AND NOT suspended) AS ready_count,
                 MIN(not_visible_until) FILTER (WHERE not_visible_until IS NOT NULL AND NOT suspended) AS next_due,
                 MAX(not_visible_until) FILTER (WHERE not_visible_until > NOW() AND NOT suspended AND (attempts > 0 OR throttled_until > NOW())) AS in_flight_until
          FROM "arbiter"."{{queue}}"
          WHERE group_key IN (SELECT group_key FROM params)
          GROUP BY group_key
        ),
        deleted AS (
          DELETE FROM "arbiter"."{{queue}}_groups" g
          WHERE g.group_key IN (SELECT group_key FROM params)
            AND NOT EXISTS (SELECT 1 FROM current c WHERE c.group_key = g.group_key)
        ),
        updated AS (
          UPDATE "arbiter"."{{queue}}_groups" g
          SET min_priority = c.min_priority,
              min_id = c.min_id,
              job_count = c.job_count,
              ready_count = c.ready_count,
              next_due = c.next_due,
              in_flight_until = c.in_flight_until
          FROM current c
          WHERE g.group_key = c.group_key
            AND (g.min_priority <> c.min_priority OR g.min_id <> c.min_id
                 OR g.job_count <> c.job_count
                 OR g.ready_count <> c.ready_count
                 OR g.next_due IS DISTINCT FROM c.next_due
                 OR g.in_flight_until IS DISTINCT FROM c.in_flight_until)
        )
        INSERT INTO "arbiter"."{{queue}}_groups" (group_key, min_priority, min_id, job_count, ready_count, next_due, in_flight_until)
        SELECT group_key,
               MIN(priority), MIN(id), COUNT(*),
               COUNT(*) FILTER (WHERE not_visible_until IS NULL AND NOT suspended),
               MIN(not_visible_until) FILTER (WHERE not_visible_until IS NOT NULL AND NOT suspended),
               MAX(not_visible_until) FILTER (WHERE not_visible_until > NOW() AND NOT suspended AND (attempts > 0 OR throttled_until > NOW()))
        FROM "arbiter"."{{queue}}"
        WHERE group_key IS NOT NULL
          AND group_key NOT IN (SELECT group_key FROM "arbiter"."{{queue}}_groups")
        GROUP BY group_key
        ON CONFLICT (group_key) DO NOTHING
