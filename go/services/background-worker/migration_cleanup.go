// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgtype"
)

type cassandraConversationCleanup interface {
	CleanupMigratedConversation(context.Context, string) error
}

func completePendingConversationDelete(ctx context.Context, id string, pending bool, cleanup cassandraConversationCleanup, markComplete func() error) error {
	if !pending {
		return nil
	}
	if cleanup == nil {
		return errors.New("Cassandra migration cleanup unavailable")
	}
	if err := cleanup.CleanupMigratedConversation(ctx, id); err != nil {
		return err
	}
	// The durable ledger must survive failed/partial Cassandra deletion. Native
	// deletion is idempotent, so a failed completion statement is safe to retry.
	return markComplete()
}

func (s *postgresStore) pendingConversationDelete(ctx context.Context, cid pgtype.UUID) (pending bool, err error) {
	err = retryPostgres(ctx, s.Pool.Reset, func() error {
		conn, err := s.acquire(ctx)
		if err != nil {
			return err
		}
		defer conn.Release()
		if err = conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conversation_migration_pending_deletes WHERE typ='conv' AND id=$1::uuid)`, cid).Scan(&pending); err != nil {
			return postgresFailure(err, "pending conversation deletion query failed")
		}
		return nil
	})
	return
}

func (s *postgresStore) markConversationDeleteComplete(ctx context.Context, cid pgtype.UUID) error {
	return retryPostgres(ctx, s.Pool.Reset, func() error {
		conn, err := s.acquire(ctx)
		if err != nil {
			return err
		}
		defer conn.Release()
		if _, err = conn.Exec(ctx, `DELETE FROM conversation_migration_pending_deletes WHERE typ='conv' AND id=$1::uuid`, cid); err != nil {
			return postgresFailure(err, "pending conversation deletion completion failed")
		}
		return nil
	})
}

func (s *postgresStore) CleanupConversation(ctx context.Context, id string) error {
	cid, err := postgresUUID(id)
	if err != nil {
		return err
	}
	pending, err := s.pendingConversationDelete(ctx, cid)
	if err != nil {
		return err
	}
	return completePendingConversationDelete(ctx, id, pending, s.CassandraCleanup, func() error {
		return s.markConversationDeleteComplete(ctx, cid)
	})
}
