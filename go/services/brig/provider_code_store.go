// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type providerVerificationCode struct {
	key, value, email string
	ttl               int32
	retries           uint8
	account           *gocql.UUID
}

func (providerVerificationCode) String() string { return "<ProviderVerificationCode>" }

const providerResetScope int32 = 3

func (s verificationCodeStore) providerLookup(ctx context.Context, key string, scope int32) (*providerVerificationCode, error) {
	var value, email *string
	var ttl, retries *int32
	var account *gocql.UUID
	err := s.cassandra.Query(`SELECT value,ttl(value),retries,email,account FROM vcodes WHERE key=? AND scope=?`, key, scope).
		WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).Scan(&value, &ttl, &retries, &email, &account)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.New("provider code lookup failed")
	}
	if value == nil || email == nil || ttl == nil || retries == nil || !validVerificationValue(*value) {
		return nil, errors.New("invalid stored provider code")
	}
	canonical, err := canonicalRegistrationEmail(*email)
	if err != nil {
		return nil, errors.New("invalid stored provider code")
	}
	return &providerVerificationCode{key: key, value: *value, email: canonical, ttl: *ttl, retries: uint8(*retries), account: account}, nil
}

func (s verificationCodeStore) providerInsert(ctx context.Context, scope int32, code providerVerificationCode) error {
	err := s.cassandra.Query(`INSERT INTO vcodes (key,scope,value,retries,email,account) VALUES (?,?,?,?,?,?) USING TTL ?`, code.key, scope, code.value, int32(code.retries), code.email, code.account, code.ttl).
		WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5}).Exec()
	return userStorageError(err)
}

func (s verificationCodeStore) providerRemove(ctx context.Context, key string, scope int32) error {
	return userStorageError(s.cassandra.Query(`DELETE FROM vcodes WHERE key=? AND scope=?`, key, scope).
		WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5}).Exec())
}

func (s verificationCodeStore) providerThrottle(ctx context.Context, key string, scope int32) (*int32, error) {
	var ttl *int32
	err := s.cassandra.Query(`SELECT ttl(initial_delay) FROM vcodes_throttle WHERE key=? AND scope=?`, key, scope).
		WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).Scan(&ttl)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, userStorageError(err)
	}
	if ttl == nil {
		return nil, errors.New("invalid stored provider throttle")
	}
	return ttl, nil
}

func (s verificationCodeStore) providerInsertThrottle(ctx context.Context, key string, scope, ttl int32) error {
	return userStorageError(s.cassandra.Query(`INSERT INTO vcodes_throttle (key,scope,initial_delay) VALUES (?,?,?) USING TTL ?`, key, scope, ttl, ttl).
		WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5}).Exec())
}
