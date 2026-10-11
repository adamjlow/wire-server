// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Native stores use text in PostgreSQL and UTF-8 blobs in Cassandra for the
// same encoded password. Do not decode/re-encode or upgrade it on a read.
type userAuthentication struct {
	Password *string
	Status   int32
}

func (userAuthentication) String() string { return "<UserAuthentication>" }

type userAuthStore struct {
	cassandra   *gocql.Session
	postgres    *pgxpool.Pool
	mode        string
	acquisition time.Duration
}

func (s userAuthStore) withPostgres(ctx context.Context, action func(*pgx.Conn) error) error {
	acquire, cancel := context.WithTimeout(ctx, s.acquisition)
	lease, err := s.postgres.Acquire(acquire)
	cancel()
	if err != nil {
		return userStorageError(err)
	}
	defer lease.Release()
	return action(lease.Conn())
}

func userStorageError(err error) error {
	if err != nil {
		// Keep the decoding location for diagnosis without the underlying error,
		// which can include a stored password or other private field value.
		var scan pgx.ScanArgError
		if errors.As(err, &scan) {
			return fmt.Errorf("user storage decoding failed at column %d", scan.ColumnIndex)
		}
		return errors.New("user storage operation failed")
	}
	return nil
}

// This UUID folding is the native Wire.MigrationLock.hashUUID contract, not
// the Hashable String/Text algorithms used by schema migrations/jobs.
func userMigrationLockKey(user gocql.UUID) int64 {
	first, second := binary.BigEndian.Uint64(user[:8]), binary.BigEndian.Uint64(user[8:])
	return int64(first ^ (second >> 32) ^ (second << 32))
}

func (s userAuthStore) withUserBackend(ctx context.Context, user gocql.UUID, action func(bool) error) error {
	switch s.mode {
	case "cassandra":
		return action(false)
	case "postgresql":
		return action(true)
	case "migration-to-postgresql":
		return s.withMigrationLock(ctx, user, func() error {
			var inPostgres bool
			err := s.withPostgres(ctx, func(conn *pgx.Conn) error {
				return conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM wire_user WHERE id=$1::uuid UNION ALL SELECT 1 FROM deleted_user WHERE id=$1::uuid)`, user.String()).Scan(&inPostgres)
			})
			if err != nil {
				return userStorageError(err)
			}
			return action(inPostgres)
		})
	default:
		return errors.New("invalid user storage mode")
	}
}

func (s userAuthStore) withMigrationLock(ctx context.Context, user gocql.UUID, action func() error) error {
	return s.withMigrationLockTimeout(ctx, user, 500*time.Millisecond, action)
}

func (s userAuthStore) withMigrationLockTimeout(ctx context.Context, user gocql.UUID, wait time.Duration, action func() error) error {
	return s.withUserMigrationLocks(ctx, []gocql.UUID{user}, wait, action)
}

func (s userAuthStore) withUserMigrationLocks(ctx context.Context, users []gocql.UUID, wait time.Duration, action func() error) error {
	// The native shared lock spans the backend lookup and complete action.
	// A canceled/failed lease is closed before reuse to release session locks.
	lockCtx, stop := context.WithTimeout(ctx, wait)
	lease, err := s.postgres.Acquire(lockCtx)
	if err != nil {
		stop()
		return userStorageError(err)
	}
	keys := make([]int64, len(users))
	for i, user := range users {
		keys[i] = userMigrationLockKey(user)
	}
	// Native sorts folded lock IDs (not UUIDs) before acquiring the full set.
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	_, err = lease.Exec(lockCtx, `SELECT pg_advisory_lock_shared(lock_id) FROM unnest($1::bigint[]) AS locks(lock_id)`, keys)
	stop()
	if err != nil {
		closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		_ = lease.Conn().Close(closeCtx)
		cancel()
		lease.Release()
		return userStorageError(err)
	}
	defer func() {
		unlock, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if _, err := lease.Exec(unlock, `SELECT pg_advisory_unlock_shared(lock_id) FROM unnest($1::bigint[]) AS locks(lock_id)`, keys); err != nil {
			_ = lease.Conn().Close(unlock)
		}
		lease.Release()
	}()
	return action()
}

func (s userAuthStore) authentication(ctx context.Context, user gocql.UUID) (*userAuthentication, error) {
	var result *userAuthentication
	err := s.withUserBackend(ctx, user, func(postgres bool) error {
		var record userAuthentication
		var status *int32
		var err error
		if postgres {
			err = s.withPostgres(ctx, func(conn *pgx.Conn) error {
				return conn.QueryRow(ctx, `SELECT password,account_status FROM wire_user WHERE id=$1::uuid`, user.String()).Scan(&record.Password, &status)
			})
		} else {
			var password []byte
			err = s.cassandra.Query(`SELECT password,status FROM user WHERE id=?`, user).
				WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).Scan(&password, &status)
			if password != nil {
				x := string(password)
				record.Password = &x
			}
		}
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, gocql.ErrNotFound) {
			return nil
		}
		if err != nil {
			return userStorageError(err)
		}
		if status != nil {
			if *status < 0 || *status > 4 {
				return errors.New("invalid stored account status")
			}
			record.Status = *status
		}
		result = &record
		return nil
	})
	return result, err
}

func (s userAuthStore) setPassword(ctx context.Context, user gocql.UUID, encoded string) error {
	return s.withUserBackend(ctx, user, func(postgres bool) error {
		if postgres {
			err := s.withPostgres(ctx, func(conn *pgx.Conn) error {
				_, err := conn.Exec(ctx, `UPDATE wire_user SET password=$2::text WHERE id=$1::uuid`, user.String(), encoded)
				return err
			})
			return userStorageError(err)
		}
		return userStorageError(s.cassandra.Query(`UPDATE user SET password=? WHERE id=?`, []byte(encoded), user).
			WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5}).Exec())
	})
}

// Native LookupHashedPassword has a password-only projection and the existing
// per-user backend/lock policy; unrelated status columns do not affect it.
func (s userAuthStore) hashedPassword(ctx context.Context, user gocql.UUID) (*string, error) {
	var result *string
	err := s.withUserBackend(ctx, user, func(postgres bool) error {
		var err error
		if postgres {
			err = s.withPostgres(ctx, func(conn *pgx.Conn) error {
				return conn.QueryRow(ctx, `SELECT password FROM wire_user WHERE id=$1::uuid`, user.String()).Scan(&result)
			})
		} else {
			var encoded []byte
			err = s.cassandra.Query(`SELECT password FROM user WHERE id=?`, user).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).Scan(&encoded)
			if encoded != nil {
				value := string(encoded)
				result = &value
			}
		}
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, gocql.ErrNotFound) {
			return nil
		}
		return userStorageError(err)
	})
	return result, err
}

// Native existence includes PostgreSQL deletion tombstones. In migration
// mode, PostgreSQL is checked first under the shared user migration lock.
func (s userAuthStore) exists(ctx context.Context, user gocql.UUID) (bool, error) {
	postgres := func() (bool, error) {
		var found bool
		err := s.withPostgres(ctx, func(conn *pgx.Conn) error {
			return conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM wire_user WHERE id=$1::uuid UNION ALL SELECT 1 FROM deleted_user WHERE id=$1::uuid)`, user.String()).Scan(&found)
		})
		return found, userStorageError(err)
	}
	cassandra := func() (bool, error) {
		var id gocql.UUID
		err := s.cassandra.Query(`SELECT id FROM user WHERE id=?`, user).
			WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).Scan(&id)
		if errors.Is(err, gocql.ErrNotFound) {
			return false, nil
		}
		return err == nil, userStorageError(err)
	}
	switch s.mode {
	case "cassandra":
		return cassandra()
	case "postgresql":
		return postgres()
	case "migration-to-postgresql":
		var found bool
		err := s.withMigrationLock(ctx, user, func() error {
			var err error
			found, err = postgres()
			if err != nil || found {
				return err
			}
			found, err = cassandra()
			return err
		})
		return found, err
	default:
		return false, errors.New("invalid user storage mode")
	}
}

func (s userAuthStore) updateAccountStatus(ctx context.Context, user gocql.UUID, status int32) error {
	if status < 0 || status > 4 {
		return errors.New("invalid account status")
	}
	return s.withUserBackend(ctx, user, func(postgres bool) error {
		if postgres {
			return userStorageError(s.withPostgres(ctx, func(conn *pgx.Conn) error {
				_, err := conn.Exec(ctx, `UPDATE wire_user SET account_status=$2::integer WHERE id=$1::uuid`, user.String(), status)
				return err
			}))
		}
		return userStorageError(s.cassandra.Query(`UPDATE user SET status=? WHERE id=?`, status, user).
			WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5}).Exec())
	})
}

func (s userAuthStore) status(ctx context.Context, user gocql.UUID) (*int32, error) {
	var status *int32
	err := s.withUserBackend(ctx, user, func(postgres bool) error {
		if postgres {
			return s.withPostgres(ctx, func(conn *pgx.Conn) error {
				err := conn.QueryRow(ctx, `SELECT account_status FROM wire_user WHERE id=$1::uuid`, user.String()).Scan(&status)
				if err != nil && !errors.Is(err, pgx.ErrNoRows) {
					return userStorageError(err)
				}
				if status != nil && (*status < 0 || *status > 4) {
					return errors.New("invalid stored account status")
				}
				var deleted bool
				if err := conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM deleted_user WHERE id=$1::uuid)`, user.String()).Scan(&deleted); err != nil {
					return userStorageError(err)
				}
				if deleted {
					x := int32(2)
					status = &x
				}
				return nil
			})
		}
		err := s.cassandra.Query(`SELECT status FROM user WHERE id=?`, user).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).Scan(&status)
		if errors.Is(err, gocql.ErrNotFound) {
			return nil
		}
		if err != nil {
			return userStorageError(err)
		}
		if status != nil && (*status < 0 || *status > 4) {
			return errors.New("invalid stored account status")
		}
		return nil
	})
	return status, err
}
