// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/jackc/pgx/v5"
)

func (s userAuthStore) activated(ctx context.Context, user gocql.UUID) (bool, error) {
	var activated bool
	err := s.withUserBackend(ctx, user, func(pg bool) error {
		if pg {
			return s.withPostgres(ctx, func(connection *pgx.Conn) error {
				err := connection.QueryRow(ctx, `SELECT activated FROM wire_user WHERE id=$1::uuid`, user.String()).Scan(&activated)
				if errors.Is(err, pgx.ErrNoRows) {
					return nil
				}
				return userStorageError(err)
			})
		}
		var value *bool
		err := s.cassandra.Query(`SELECT activated FROM user WHERE id=?`, user).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).Scan(&value)
		if errors.Is(err, gocql.ErrNotFound) {
			return nil
		}
		if err != nil {
			return userStorageError(err)
		}
		if value == nil {
			return errors.New("invalid stored activation state")
		}
		activated = *value
		return nil
	})
	return activated, err
}
func (s userAuthStore) emailKeyAvailable(ctx context.Context, email string, user *gocql.UUID) (bool, error) {
	key, err := emailUniqueKey(email)
	if err != nil {
		return false, err
	}
	owner, err := s.cassandraLoginLookup(ctx, `SELECT user FROM user_keys WHERE key=?`, key)
	if err != nil {
		return false, err
	}
	if owner == nil || user != nil && *owner == *user {
		return true, nil
	}
	active, err := s.activated(ctx, *owner)
	return !active, err
}
func (s userAuthStore) claimEmailKey(ctx context.Context, email string, user gocql.UUID) (bool, error) {
	available, err := s.emailKeyAvailable(ctx, email, &user)
	if err != nil || !available {
		return available, err
	}
	key, err := emailUniqueKey(email)
	if err != nil {
		return false, err
	}
	// Preserve native availability check followed by a non-LWT insert. A new
	// transactional uniqueness policy would change upstream race behavior.
	err = s.cassandra.Query(`INSERT INTO user_keys (key,user) VALUES (?,?)`, key, user).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5}).Exec()
	return true, userStorageError(err)
}
func (s userAuthStore) deleteEmailKey(ctx context.Context, email string, onlyOwner *gocql.UUID) error {
	key, err := emailUniqueKey(email)
	if err != nil {
		return err
	}
	if onlyOwner != nil {
		owner, err := s.cassandraLoginLookup(ctx, `SELECT user FROM user_keys WHERE key=?`, key)
		if err != nil {
			return err
		}
		if owner == nil || *owner != *onlyOwner {
			return nil
		}
	}
	return userStorageError(s.cassandra.Query(`DELETE FROM user_keys WHERE key=?`, key).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5}).Exec())
}
func (s userAuthStore) setEmailIdentity(ctx context.Context, user gocql.UUID, email *string, activate bool) error {
	return s.withUserBackend(ctx, user, func(pg bool) error {
		if pg {
			return s.withPostgres(ctx, func(connection *pgx.Conn) error {
				query := `UPDATE wire_user SET email=$2::text WHERE id=$1::uuid`
				if activate {
					query = `UPDATE wire_user SET activated=true,email=$2::text WHERE id=$1::uuid`
				}
				_, err := connection.Exec(ctx, query, user.String(), email)
				return userStorageError(err)
			})
		}
		query := `UPDATE user SET email=? WHERE id=?`
		if activate {
			query = `UPDATE user SET activated=true,email=? WHERE id=?`
		}
		return userStorageError(s.cassandra.Query(query, email, user).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5}).Exec())
	})
}
func (s userAuthStore) clearUnvalidatedEmail(ctx context.Context, user gocql.UUID) error {
	return s.withUserBackend(ctx, user, func(pg bool) error {
		if pg {
			return s.withPostgres(ctx, func(connection *pgx.Conn) error {
				_, err := connection.Exec(ctx, `UPDATE wire_user SET email_unvalidated=NULL WHERE id=$1::uuid`, user.String())
				return userStorageError(err)
			})
		}
		return userStorageError(s.cassandra.Query(`UPDATE user SET email_unvalidated=null WHERE id=?`, user).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5}).Exec())
	})
}
