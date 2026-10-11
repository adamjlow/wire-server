// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"crypto/rand"
	"errors"
	"math/big"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type prekeyClaimStore struct {
	store         clientStore
	random        bool
	gate          chan struct{}
	version       func(context.Context, gocql.UUID, string) *uint32
	advance       func(context.Context, gocql.UUID, string, *uint32) bool
	attemptFailed func()
	exhausted     func()
}

func (s prekeyClaimStore) claim(ctx context.Context, user gocql.UUID, id string) (*uncheckedPrekey, error) {
	if s.random {
		select {
		case <-s.gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		defer func() { s.gate <- struct{}{} }()
		keys, err := s.selectKeys(ctx, user, id, false)
		if err != nil {
			return nil, err
		}
		key, err := pickNativeRandomPrekey(keys)
		if err != nil {
			return nil, err
		}
		return s.removeAndReturn(ctx, user, id, key)
	}
	return optimisticPrekeyClaim(ctx, func() *uint32 { return s.version(ctx, user, id) }, func() (*uncheckedPrekey, error) {
		keys, err := s.selectKeys(ctx, user, id, true)
		if err != nil {
			return nil, err
		}
		var key *uncheckedPrekey
		if len(keys) > 0 {
			key = &keys[0]
		}
		return s.removeAndReturn(ctx, user, id, key)
	}, func(version *uint32) bool { return s.advance(ctx, user, id, version) }, s.attemptFailed, s.exhausted)
}

func optimisticPrekeyClaim(ctx context.Context, version func() *uint32, claim func() (*uncheckedPrekey, error), advance func(*uint32) bool, attemptFailed, exhausted func()) (*uncheckedPrekey, error) {
	for n := 10; ; n-- {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		current := version()
		key, err := claim()
		if err != nil {
			return nil, err
		}
		if advance(current) {
			return key, nil
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if n == 0 {
			if exhausted != nil {
				exhausted()
			}
			return key, nil
		}
		if attemptFailed != nil {
			attemptFailed()
		}
	}
}

func pickNativeRandomPrekey(keys []uncheckedPrekey) (*uncheckedPrekey, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	if len(keys) == 1 {
		return &keys[0], nil
	}
	ordinary := make([]uncheckedPrekey, 0, len(keys))
	for _, key := range keys {
		if key.ID != 65535 {
			ordinary = append(ordinary, key)
		}
	}
	if len(ordinary) == 0 {
		return nil, errors.New("invalid stored prekey selection")
	}
	index, err := rand.Int(rand.Reader, big.NewInt(int64(len(ordinary))))
	if err != nil {
		return nil, errors.New("prekey selection failed")
	}
	return &ordinary[index.Int64()], nil
}

func (s prekeyClaimStore) selectKeys(ctx context.Context, user gocql.UUID, id string, first bool) ([]uncheckedPrekey, error) {
	query := `SELECT key,data FROM prekeys WHERE user=? AND client=?`
	if first {
		query += ` LIMIT 1`
	}
	iter := s.store.cassandra.Query(query, user, id).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).Iter()
	result := []uncheckedPrekey{}
	var key int32
	var data string
	for iter.Scan(&key, &data) {
		result = append(result, uncheckedPrekey{ID: uint16(key), Key: data})
	}
	if err := iter.Close(); err != nil {
		return nil, userStorageError(err)
	}
	return result, nil
}

func (s prekeyClaimStore) removeAndReturn(ctx context.Context, user gocql.UUID, id string, key *uncheckedPrekey) (*uncheckedPrekey, error) {
	if key == nil || key.ID == 65535 {
		return key, nil
	}
	err := s.store.cassandra.Query(`DELETE FROM prekeys WHERE user=? AND client=? AND key=?`, user, id, int32(key.ID)).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).Exec()
	if err != nil {
		return nil, userStorageError(err)
	}
	return key, nil
}
