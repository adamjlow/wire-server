// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"math/big"
	"net/mail"
	"unicode/utf8"

	"github.com/apache/cassandra-gocql-driver/v2"
)

var errInvalidActivationCode = errors.New("invalid activation code")

func activationEmailKey(email string) (string, error) {
	key, err := emailUniqueKey(email)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(key))
	return base64.URLEncoding.EncodeToString(digest[:]), nil
}

func cassandraTTL(seconds float64) (int32, error) {
	rounded := math.RoundToEven(seconds)
	if math.IsNaN(rounded) || math.IsInf(rounded, 0) || rounded >= float64(math.MaxInt64) || rounded < float64(math.MinInt64) {
		return 0, errors.New("invalid Cassandra TTL")
	}
	return int32(int64(rounded)), nil
}

type activationRecord struct {
	key, scope, email, code string
	user                    *gocql.UUID
	retries, ttl            int32
}

func (activationRecord) String() string { return "<ActivationRecord>" }

type activationStore struct{ cassandra *gocql.Session }

func validActivationValue(value string) bool {
	for _, ch := range value {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-' || ch == '=') {
			return false
		}
	}
	return true
}

func (s activationStore) newCode(ctx context.Context, email string, seconds float64, user *gocql.UUID) (activationRecord, error) {
	key, err := activationEmailKey(email)
	if err != nil {
		return activationRecord{}, err
	}
	ttl, err := cassandraTTL(seconds)
	if err != nil {
		return activationRecord{}, err
	}
	number, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return activationRecord{}, errors.New("activation randomness unavailable")
	}
	record := activationRecord{key: key, scope: "email", email: email, code: fmt.Sprintf("%06d", number.Int64()), user: user, retries: 3, ttl: ttl}
	return record, s.insert(ctx, record)
}
func (s activationStore) insert(ctx context.Context, record activationRecord) error {
	err := s.cassandra.Query(`INSERT INTO activation_keys (key,key_type,key_text,code,user,retries) VALUES (?,?,?,?,?,?) USING TTL ?`, record.key, record.scope, record.email, record.code, record.user, record.retries, record.ttl).
		WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5}).Exec()
	if err != nil {
		return errors.New("activation code write failed")
	}
	return nil
}
func (s activationStore) lookup(ctx context.Context, key string) (*activationRecord, error) {
	var ttl, retries *int32
	var scope, email, code *string
	var user *gocql.UUID
	err := s.cassandra.Query(`SELECT ttl(code) AS ttl,key_type,key_text,code,user,retries FROM activation_keys WHERE key=?`, key).
		WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).Scan(&ttl, &scope, &email, &code, &user, &retries)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.New("activation code read failed")
	}
	if ttl == nil || scope == nil || email == nil || code == nil || retries == nil {
		return nil, errors.New("invalid stored activation code")
	}
	if !validActivationValue(*code) {
		return nil, errors.New("invalid stored activation code encoding")
	}
	for _, ch := range *scope {
		if ch > 127 {
			return nil, errors.New("invalid stored activation scope")
		}
	}
	return &activationRecord{key: key, scope: *scope, email: *email, code: *code, user: user, retries: *retries, ttl: *ttl}, nil
}

func (s activationStore) delete(ctx context.Context, key string) error {
	// Native deleteActivationPair deliberately has no retry wrapper.
	if err := s.cassandra.Query(`DELETE FROM activation_keys WHERE key=?`, key).WithContext(ctx).Consistency(gocql.LocalQuorum).Exec(); err != nil {
		return errors.New("activation code deletion failed")
	}
	return nil
}
func (s activationStore) verify(ctx context.Context, key, code string) (*activationRecord, error) {
	record, err := s.lookup(ctx, key)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, errInvalidActivationCode
	}
	if subtle.ConstantTimeCompare([]byte(record.code), []byte(code)) == 1 {
		// Native success precedes retries and keeps the record for repeat links.
		parsed, err := mail.ParseAddress(record.email)
		if record.scope != "email" || !utf8.ValidString(record.email) || err != nil || parsed.Address != record.email {
			return nil, errInvalidActivationCode
		}
		return record, nil
	}
	if record.retries >= 1 {
		record.retries--
		err = s.insert(ctx, *record)
	} else {
		err = s.delete(ctx, key)
	}
	if err != nil {
		return nil, err
	}
	return nil, errInvalidActivationCode
}

// Native LookupActivationCode reads only user and code, independently of the
// activation verifier's scope/retry/TTL projection.
func (s activationStore) lookupPair(ctx context.Context, email string) (map[string]string, error) {
	key, err := activationEmailKey(email)
	if err != nil {
		return nil, err
	}
	var user *gocql.UUID
	var code *string
	err = s.cassandra.Query(`SELECT user,code FROM activation_keys WHERE key=?`, key).
		WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).Scan(&user, &code)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, nil
	}
	if err != nil || code == nil || !validActivationValue(*code) {
		return nil, errors.New("activation pair read failed")
	}
	return map[string]string{"key": key, "code": *code}, nil
}
