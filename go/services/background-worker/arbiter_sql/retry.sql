-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025 Joshua Miller
-- Adapted from Arbiter.Core.Sql.Lifecycle at b9c57eb1f8277d97616aa449bea471fe9ce14eda.
-- Optimistic attempt guard prevents an old handler changing a reclaimed job.
UPDATE "arbiter"."{{queue}}"
SET not_visible_until=NOW() + ($3::bigint * interval '1 second'),
    last_error=$4::text,
    updated_at=NOW(),
    claimed_by=NULL
WHERE id=$1::bigint AND attempts=$2::int AND NOT suspended
