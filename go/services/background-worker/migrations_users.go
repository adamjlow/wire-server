// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/jackc/pgx/v5/pgtype"
)

func (s *Service) migrateUsers(ctx context.Context, cassandra *gocql.Session, postgres *postgresStore) error {
	page := cassandraMigrationPage(cassandra, `SELECT id FROM user`, "id")
	page = validatedMigrationPage(page, func(row migrationRow) error {
		id, ok := cqlUUID(row["id"])
		if !ok || !id.Valid {
			return invalidUserMigrationRow()
		}
		return nil
	})
	query := userMigrationCassandraQuery(cassandra)
	return s.migratePhases(ctx, "users", []migrationPhase{{page: page, parallel: true, write: func(ctx context.Context, row migrationRow) (bool, error) {
		id, _ := cqlUUID(row["id"])
		return s.copyLockedUserMigration(ctx, id,
			func(ctx context.Context, action func() error) error {
				return postgres.withMigrationLock(ctx, postgresUUIDString(id), true, 10*time.Second, action)
			},
			func(ctx context.Context) (*userMigrationRecord, error) { return readUserMigration(ctx, id, query) },
			postgres,
			func(ctx context.Context, record userMigrationRecord) error {
				return deleteMigratedUser(ctx, cassandra, record)
			})
	}}})
}

func (s *Service) copyLockedUserMigration(ctx context.Context, id pgtype.UUID,
	lock func(context.Context, func() error) error,
	read func(context.Context) (*userMigrationRecord, error),
	store userMigrationStore,
	cleanup func(context.Context, userMigrationRecord) error,
) (counted bool, err error) {
	if !id.Valid {
		return false, invalidUserMigrationRow()
	}
	err = s.timedExclusiveMigration(ctx, "users", lock, func(attempt context.Context) error {
		record, err := read(attempt)
		if err != nil || record == nil {
			return err
		}
		counted, err = copyUserMigration(attempt, *record, store, cleanup)
		return err
	})
	return
}
