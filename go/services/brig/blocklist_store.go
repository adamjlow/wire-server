// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/jackc/pgx/v5"
)

// Block-list migration has its own selector. It reads Cassandra and mirrors
// writes to PostgreSQL; user migration's PG-priority rule does not apply here.
type blocklistStore struct {
	databases userAuthStore
	mode      string
}

func (s blocklistStore) exists(ctx context.Context, email string) (bool, error) {
	key, err := emailUniqueKey(email)
	if err != nil {
		return false, err
	}
	switch s.mode {
	case "postgresql":
		var exists bool
		err := s.databases.withPostgres(ctx, func(connection *pgx.Conn) error {
			return userStorageError(connection.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM email_blocklist WHERE email=$1)`, key).Scan(&exists))
		})
		return exists, err
	case "cassandra", "migration-to-postgresql":
		var value string
		err := s.databases.cassandra.Query(`SELECT key FROM blacklist WHERE key=?`, key).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).Scan(&value)
		if errors.Is(err, gocql.ErrNotFound) {
			return false, nil
		}
		return err == nil, userStorageError(err)
	default:
		return false, errors.New("invalid blocklist storage mode")
	}
}
func (s blocklistStore) write(ctx context.Context, email string, remove bool) error {
	key, err := emailUniqueKey(email)
	if err != nil {
		return err
	}
	switch s.mode {
	case "cassandra", "migration-to-postgresql":
		query := `INSERT INTO blacklist (key) VALUES (?)`
		if remove {
			query = `DELETE FROM blacklist WHERE key=?`
		}
		if err := s.databases.cassandra.Query(query, key).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5}).Exec(); err != nil {
			return userStorageError(err)
		}
		if s.mode == "cassandra" {
			return nil
		}
	case "postgresql":
	default:
		return errors.New("invalid blocklist storage mode")
	}
	return s.databases.withPostgres(ctx, func(connection *pgx.Conn) error {
		query := `INSERT INTO email_blocklist (email) VALUES ($1) ON CONFLICT DO NOTHING`
		if remove {
			query = `DELETE FROM email_blocklist WHERE email=$1`
		}
		_, err := connection.Exec(ctx, query, key)
		return userStorageError(err)
	})
}
