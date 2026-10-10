// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"strings"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func emailVerificationKey(email string) (string, error) {
	local, domain, ok := strings.Cut(email, "@")
	if !ok || local == "" || domain == "" {
		return "", errors.New("invalid stored email")
	}
	// Native trusted-domain comparison is case sensitive before lowercasing.
	if domain != "wearezeta.com" && domain != "wire.com" && domain != "simulator.amazonses.com" {
		local, _, _ = strings.Cut(local, "+")
	}
	digest := sha256.Sum256([]byte(strings.ToLower(local) + "@" + strings.ToLower(domain)))
	return base64.URLEncoding.EncodeToString(digest[:15]), nil
}

func verificationScope(action string) (int32, bool) {
	switch action {
	case "create_scim_token":
		return 6, true
	case "login":
		return 4, true
	case "delete_team":
		return 7, true
	default:
		return 0, false
	}
}

type verificationCodeStore struct{ cassandra *gocql.Session }

// Successful verification keeps the code; failed attempts decrement retries
// and reinsert all fields using the remaining native Cassandra TTL.
func (s verificationCodeStore) verify(ctx context.Context, email, action, supplied string) (bool, error) {
	key, err := emailVerificationKey(email)
	if err != nil {
		return false, err
	}
	scope, ok := verificationScope(action)
	if !ok {
		return false, errors.New("invalid verification action")
	}
	var value, storedEmail *string
	var ttl, retries *int32
	var account *gocql.UUID
	err = s.cassandra.Query(`SELECT value,ttl(value),retries,email,account FROM vcodes WHERE key=? AND scope=?`, key, scope).
		WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).Scan(&value, &ttl, &retries, &storedEmail, &account)
	if errors.Is(err, gocql.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, errors.New("verification code lookup failed")
	}
	if value == nil || storedEmail == nil || ttl == nil || retries == nil {
		return false, errors.New("invalid stored verification code")
	}
	remaining := uint8(*retries) // Native Retries.fromCql wraps Int32 to Word8.
	if remaining == 0 {
		return false, nil
	}
	if subtle.ConstantTimeCompare([]byte(*value), []byte(supplied)) == 1 {
		return true, nil
	}
	err = s.cassandra.Query(`INSERT INTO vcodes (key,scope,value,retries,email,account) VALUES (?,?,?,?,?,?) USING TTL ?`, key, scope, *value, int32(remaining-1), *storedEmail, account, *ttl).
		WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5}).Exec()
	if err != nil {
		return false, errors.New("verification code update failed")
	}
	return false, nil
}
