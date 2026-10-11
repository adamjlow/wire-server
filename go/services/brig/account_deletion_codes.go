// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type deletionVerificationCode struct {
	key, value, email string
	ttl               int32
	retries           uint8
	account           *gocql.UUID
}

func (deletionVerificationCode) String() string { return "<DeletionVerificationCode>" }

type deletionCodePending struct{ ttl int32 }

func (deletionCodePending) Error() string { return "deletion code pending" }

type deletionCodeThrottled struct{ ttl int32 }

func (deletionCodeThrottled) Error() string { return "verification code throttled" }

// Callbacks preserve the native nontransactional ordering. An existing code,
// including one with exhausted retries, takes precedence over throttling.
type accountDeletionCodes struct {
	lookup         func(context.Context, string) (*deletionVerificationCode, error)
	insert         func(context.Context, deletionVerificationCode) error
	remove         func(context.Context, string) error
	throttle       func(context.Context, string) (*int32, error)
	insertThrottle func(context.Context, string, int32) error
	mail           func(context.Context, selfProfile, string, string) error
	throttleTTL    int32
}

func (s accountDeletionCodes) send(ctx context.Context, profile selfProfile, account gocql.UUID) (int64, error) {
	if s.lookup == nil || s.insert == nil || s.remove == nil || s.throttle == nil || s.insertThrottle == nil || s.mail == nil || profile.Email == nil {
		return 0, errors.New("deletion verification unavailable")
	}
	key, err := emailVerificationKey(*profile.Email)
	if err != nil {
		return 0, err
	}
	existing, err := s.lookup(ctx, key)
	if err != nil {
		return 0, err
	}
	if existing != nil {
		return 0, deletionCodePending{existing.ttl}
	}
	var random [15]byte
	if _, err := rand.Read(random[:]); err != nil {
		return 0, errors.New("verification code generation failed")
	}
	code := deletionVerificationCode{key: key, value: base64.URLEncoding.EncodeToString(random[:]), email: *profile.Email, ttl: 600, retries: 3, account: &account}
	delay, err := s.throttle(ctx, key)
	if err != nil {
		return 0, err
	}
	if delay != nil {
		return 0, deletionCodeThrottled{*delay}
	}
	if err := s.insertThrottle(ctx, key, s.throttleTTL); err != nil {
		return 0, err
	}
	if err := s.insert(ctx, code); err != nil {
		return 0, err
	}
	if err := s.mail(ctx, profile, key, code.value); err != nil {
		// Native onException removes only the code, retaining the throttle.
		if cleanup := s.remove(ctx, key); cleanup != nil {
			return 0, cleanup
		}
		return 0, err
	}
	return 600, nil
}

func (s accountDeletionCodes) verify(ctx context.Context, key, supplied string) (*deletionVerificationCode, error) {
	if s.lookup == nil || s.insert == nil {
		return nil, errors.New("deletion verification unavailable")
	}
	code, err := s.lookup(ctx, key)
	if err != nil || code == nil {
		return nil, err
	}
	if code.retries == 0 {
		return nil, nil
	}
	if subtle.ConstantTimeCompare([]byte(code.value), []byte(supplied)) == 1 {
		return code, nil
	}
	updated := *code
	updated.retries--
	if err := s.insert(ctx, updated); err != nil {
		return nil, err
	}
	return nil, nil
}

func (s verificationCodeStore) deletionLookup(ctx context.Context, key string) (*deletionVerificationCode, error) {
	var value, email *string
	var ttl, retries *int32
	var account *gocql.UUID
	err := s.cassandra.Query(`SELECT value,ttl(value),retries,email,account FROM vcodes WHERE key=? AND scope=?`, key, int32(1)).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).Scan(&value, &ttl, &retries, &email, &account)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.New("verification code lookup failed")
	}
	if value == nil || email == nil || ttl == nil || retries == nil {
		return nil, errors.New("invalid stored verification code")
	}
	return &deletionVerificationCode{key: key, value: *value, email: *email, ttl: *ttl, retries: uint8(*retries), account: account}, nil
}

func (s verificationCodeStore) deletionInsert(ctx context.Context, code deletionVerificationCode) error {
	err := s.cassandra.Query(`INSERT INTO vcodes (key,scope,value,retries,email,account) VALUES (?,?,?,?,?,?) USING TTL ?`, code.key, int32(1), code.value, int32(code.retries), code.email, code.account, code.ttl).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5}).Exec()
	if err != nil {
		return errors.New("verification code update failed")
	}
	return nil
}

func (s verificationCodeStore) deletionRemove(ctx context.Context, key string) error {
	err := s.cassandra.Query(`DELETE FROM vcodes WHERE key=? AND scope=?`, key, int32(1)).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5}).Exec()
	if err != nil {
		return errors.New("verification code deletion failed")
	}
	return nil
}

func (s verificationCodeStore) deletionThrottle(ctx context.Context, key string) (*int32, error) {
	var ttl *int32
	err := s.cassandra.Query(`SELECT ttl(initial_delay) FROM vcodes_throttle WHERE key=? AND scope=?`, key, int32(1)).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).Scan(&ttl)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.New("verification throttle lookup failed")
	}
	if ttl == nil {
		return nil, errors.New("invalid stored verification throttle")
	}
	return ttl, nil
}

func (s verificationCodeStore) deletionInsertThrottle(ctx context.Context, key string, ttl int32) error {
	err := s.cassandra.Query(`INSERT INTO vcodes_throttle (key,scope,initial_delay) VALUES (?,?,?) USING TTL ?`, key, int32(1), ttl, ttl).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5}).Exec()
	if err != nil {
		return errors.New("verification throttle update failed")
	}
	return nil
}
