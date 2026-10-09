-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025 Joshua Miller
-- Adapted from Arbiter.Core.Sql.Tree.readChildResultsSQL at b9c57eb1f8277d97616aa449bea471fe9ce14eda.
SELECT 'r'::text AS source, child_id, result, NULL::text AS error, NULL::bigint AS dlq_pk FROM "arbiter"."{{queue}}_results" WHERE parent_id = $1::bigint
        UNION ALL
        SELECT 'e' AS source, job_id AS child_id, NULL::jsonb AS result, last_error AS error, id AS dlq_pk FROM "arbiter"."{{queue}}_dlq" WHERE parent_id = $1::bigint
        UNION ALL
        SELECT 's' AS source, NULL::bigint AS child_id, parent_state AS result, NULL::text AS error, NULL::bigint AS dlq_pk FROM "arbiter"."{{queue}}" WHERE id = $1::bigint AND parent_state IS NOT NULL
