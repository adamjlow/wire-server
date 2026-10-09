-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025 Joshua Miller
-- Adapted from Arbiter.Core.Sql.Archive.purgeArchiveSQL at b9c57eb1f8277d97616aa449bea471fe9ce14eda.
DELETE FROM "arbiter"."{{queue}}_archive"
        WHERE ctid IN (
          SELECT ctid FROM "arbiter"."{{queue}}_archive"
          WHERE archive_expires_at < NOW()
          LIMIT 10000
        )
