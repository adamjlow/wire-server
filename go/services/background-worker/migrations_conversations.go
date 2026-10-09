// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"errors"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/jackc/pgx/v5/pgtype"
)

type migratedConversationCleanup struct {
	Cassandra cassandraConversationStore
	Postgres  *postgresStore
}

func (s migratedConversationCleanup) CleanupMigratedConversation(ctx context.Context, id string) error {
	cid, err := postgresUUID(id)
	if err != nil {
		return err
	}
	row, err := s.Cassandra.readMigration(ctx, cid, s.Postgres)
	if err != nil || row == nil {
		return err
	}
	return s.Cassandra.deleteMigration(ctx, *row)
}

func (s cassandraConversationStore) readMigration(ctx context.Context, id pgtype.UUID, postgres *postgresStore) (*conversationMigrationRecord, error) {
	return readConversationMigration(ctx, id, conversationMigrationCassandraQuery(s.Session),
		func(ctx context.Context, group []byte) ([]conversationMigrationMLSClient, error) {
			return s.migrationMLSClients(ctx, group, postgres)
		}, func(ctx context.Context) error { return s.DeleteConversation(ctx, postgresUUIDString(id)) })
}

func (s cassandraConversationStore) deleteMigration(ctx context.Context, row conversationMigrationRecord) error {
	cid := gocql.UUID(row.ID.Bytes)
	for _, sub := range row.Subs {
		if err := s.query(ctx, `DELETE FROM mls_group_member_client WHERE group_id=?`, sub.GroupID).RetryPolicy(cassandraRetry(5)).Exec(); err != nil {
			return errors.New("Cassandra migrated subconversation clients deletion failed")
		}
		if err := s.query(ctx, `DELETE FROM subconversation WHERE conv_id=? AND subconv_id=?`, cid, sub.ID).RetryPolicy(cassandraRetry(5)).Exec(); err != nil {
			return errors.New("Cassandra migrated subconversation deletion failed")
		}
	}
	if row.Metadata.Protocol != nil && *row.Metadata.Protocol != 0 {
		// This also removes main clients when group info was absent and no
		// main client rows were copied, exactly as native deleteConv does.
		if err := s.query(ctx, `DELETE FROM mls_group_member_client WHERE group_id=?`, row.Metadata.GroupID).RetryPolicy(cassandraRetry(5)).Exec(); err != nil {
			return errors.New("Cassandra migrated conversation clients deletion failed")
		}
	}
	if row.Metadata.Team.Valid {
		batch := s.Session.NewBatch(gocql.LoggedBatch).WithContext(ctx)
		batch.Cons = gocql.LocalQuorum
		batch.RetryPolicy(cassandraRetry(5))
		batch.Query(`UPDATE conversation SET deleted=true WHERE conv=?`, cid)
		batch.Query(`DELETE FROM team_conv WHERE team=? AND conv=?`, gocql.UUID(row.Metadata.Team.Bytes), cid)
		if err := s.Session.ExecuteBatch(batch); err != nil {
			return errors.New("Cassandra migrated team conversation deletion failed")
		}
	}
	return s.DeleteConversation(ctx, postgresUUIDString(row.ID))
}

func (s *Service) copyConversationMigration(ctx context.Context,
	lock func(context.Context, func() error) error,
	read func(context.Context) (*conversationMigrationRecord, error),
	save, cleanup func(context.Context, conversationMigrationRecord) error,
	complete func(context.Context) error,
) (bool, error) {
	err := s.timedExclusiveMigration(ctx, "local_convs", lock, func(attempt context.Context) error {
		row, err := read(attempt)
		if err != nil {
			return err
		}
		if row != nil {
			if err := save(attempt, *row); err != nil {
				return err
			}
			if err := cleanup(attempt, *row); err != nil {
				return err
			}
		}
		// Unlike remote membership, completion stays within the exclusive
		// lock and row timeout. Missing/incomplete rows still count in native.
		return complete(attempt)
	})
	return err == nil, err
}

func (s *Service) migrateConversations(ctx context.Context, cassandra *gocql.Session, postgres *postgresStore) error {
	page := validatedMigrationPage(cassandraMigrationPage(cassandra, `SELECT conv FROM conversation`, "conv"), func(row migrationRow) error {
		id, ok := cqlUUID(row["conv"])
		if !ok || !id.Valid {
			return errors.New("invalid conversation migration identifier")
		}
		return nil
	})
	store := cassandraConversationStore{Session: cassandra}
	return s.migratePhases(ctx, "local_convs", []migrationPhase{{page: page, parallel: true, write: func(ctx context.Context, row migrationRow) (bool, error) {
		id, _ := cqlUUID(row["conv"])
		return s.copyConversationMigration(ctx,
			func(ctx context.Context, action func() error) error {
				return postgres.withMigrationLock(ctx, postgresUUIDString(id), true, 10*time.Second, action)
			},
			func(ctx context.Context) (*conversationMigrationRecord, error) {
				return store.readMigration(ctx, id, postgres)
			},
			postgres.saveConversationMigration, store.deleteMigration,
			func(ctx context.Context) error { return postgres.markConversationDeleteComplete(ctx, id) })
	}}})
}
