// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"

	"github.com/apache/cassandra-gocql-driver/v2"
)

// Native properties stay in Cassandra independently of the user-store selector.
type propertyStore struct{ cassandra *gocql.Session }

type propertyPersistence interface {
	lookup(context.Context, gocql.UUID, string) ([]byte, bool, error)
	count(context.Context, gocql.UUID) (int64, error)
	write(context.Context, gocql.UUID, string, []byte) error
	remove(context.Context, gocql.UUID, string) error
	clear(context.Context, gocql.UUID) error
	keys(context.Context, gocql.UUID) ([]string, error)
	all(context.Context, gocql.UUID) (map[string][]byte, error)
}

func (s propertyStore) query(ctx context.Context, retries int, statement string, values ...any) *gocql.Query {
	return s.cassandra.Query(statement, values...).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: retries})
}

func (s propertyStore) lookup(ctx context.Context, user gocql.UUID, key string) ([]byte, bool, error) {
	var value []byte
	err := s.query(ctx, 1, `SELECT value FROM properties WHERE user=? AND key=?`, user, key).Scan(&value)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, false, nil
	}
	if err == nil && value == nil {
		return nil, false, errors.New("property blob decoding failed")
	}
	return value, err == nil, userStorageError(err)
}

func (s propertyStore) count(ctx context.Context, user gocql.UUID) (int64, error) {
	var count int64
	err := s.query(ctx, 1, `SELECT COUNT(*) FROM properties WHERE user=?`, user).Scan(&count)
	if errors.Is(err, gocql.ErrNotFound) {
		return 0, nil
	}
	return count, userStorageError(err)
}

func (s propertyStore) write(ctx context.Context, user gocql.UUID, key string, value []byte) error {
	return userStorageError(s.query(ctx, 5, `INSERT INTO properties (user,key,value) VALUES (?,?,?)`, user, key, value).Exec())
}

func (s propertyStore) remove(ctx context.Context, user gocql.UUID, key string) error {
	return userStorageError(s.query(ctx, 5, `DELETE FROM properties WHERE user=? AND key=?`, user, key).Exec())
}

func (s propertyStore) clear(ctx context.Context, user gocql.UUID) error {
	return userStorageError(s.query(ctx, 5, `DELETE FROM properties WHERE user=?`, user).Exec())
}

func (s propertyStore) keys(ctx context.Context, user gocql.UUID) ([]string, error) {
	iter := s.query(ctx, 1, `SELECT key FROM properties WHERE user=?`, user).Iter()
	result := []string{}
	var key *string
	for iter.Scan(&key) {
		if key == nil || !validPropertyKey(*key) {
			_ = iter.Close()
			return nil, errors.New("property key decoding failed")
		}
		result = append(result, *key)
	}
	return result, userStorageError(iter.Close())
}

func (s propertyStore) all(ctx context.Context, user gocql.UUID) (map[string][]byte, error) {
	iter := s.query(ctx, 1, `SELECT key,value FROM properties WHERE user=?`, user).Iter()
	result := map[string][]byte{}
	var key *string
	var value []byte
	for iter.Scan(&key, &value) {
		if key == nil || !validPropertyKey(*key) || value == nil {
			_ = iter.Close()
			return nil, errors.New("property row decoding failed")
		}
		result[*key] = append([]byte{}, value...)
	}
	return result, userStorageError(iter.Close())
}
