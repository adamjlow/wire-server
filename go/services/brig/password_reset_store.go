// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/jackc/pgx/v5"
)

func passwordResetKey(user gocql.UUID) string {
	digest := sha256.Sum256([]byte(user.String()))
	return base64.URLEncoding.EncodeToString(digest[:])
}
func newPasswordResetCode() (string, error) {
	var random [24]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", errors.New("password reset randomness unavailable")
	}
	return base64.URLEncoding.EncodeToString(random[:]), nil
}

type passwordResetRecord struct {
	code    string
	user    gocql.UUID
	retries *int32
	timeout *time.Time
}

func (passwordResetRecord) String() string { return "<PasswordResetRecord>" }

type passwordResetStore struct {
	databases userAuthStore
	mode      string
}

func (s passwordResetStore) lookup(ctx context.Context, key string) (*passwordResetRecord, error) {
	var record passwordResetRecord
	switch s.mode {
	case "postgresql":
		found := true
		err := s.databases.withPostgres(ctx, func(connection *pgx.Conn) error {
			var user string
			err := connection.QueryRow(ctx, `SELECT code,user_id::text,retries,timeout FROM password_reset WHERE key=$1 AND expires_at>now()`, key).Scan(&record.code, &user, &record.retries, &record.timeout)
			if errors.Is(err, pgx.ErrNoRows) {
				found = false
				return nil
			}
			if err != nil {
				return userStorageError(err)
			}
			record.user, err = gocql.ParseUUID(user)
			if err != nil {
				return errors.New("invalid stored password reset user")
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, nil
		}
	case "cassandra", "migration-to-postgresql":
		var code *string
		var user *gocql.UUID
		err := s.databases.cassandra.Query(`SELECT code,user,retries,timeout FROM password_reset WHERE key=?`, key).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).Scan(&code, &user, &record.retries, &record.timeout)
		if errors.Is(err, gocql.ErrNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, userStorageError(err)
		}
		if code == nil || user == nil {
			return nil, errors.New("incomplete stored password reset")
		}
		record.code, record.user = *code, *user
	default:
		return nil, errors.New("invalid password reset storage mode")
	}
	if !validActivationValue(record.code) {
		return nil, errors.New("invalid stored password reset code")
	}
	return &record, nil
}
func (s passwordResetStore) insert(ctx context.Context, key string, record passwordResetRecord, ttl int32) error {
	if record.retries == nil || record.timeout == nil {
		return errors.New("missing password reset insertion fields")
	}
	switch s.mode {
	case "cassandra", "migration-to-postgresql":
		if err := s.databases.cassandra.Query(`INSERT INTO password_reset (key,code,user,retries,timeout) VALUES (?,?,?,?,?) USING TTL ?`, key, record.code, record.user, *record.retries, *record.timeout, ttl).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5}).Exec(); err != nil {
			return userStorageError(err)
		}
		if s.mode == "cassandra" {
			return nil
		}
	case "postgresql":
	default:
		return errors.New("invalid password reset storage mode")
	}
	return s.databases.withPostgres(ctx, func(connection *pgx.Conn) error {
		_, err := connection.Exec(ctx, `INSERT INTO password_reset (key,code,user_id,retries,timeout,expires_at) VALUES ($1,$2,$3::uuid,$4::integer,$5::timestamptz,now()+make_interval(secs=>$6::integer)) ON CONFLICT(key) DO UPDATE SET code=$2,user_id=$3::uuid,retries=$4::integer,timeout=$5::timestamptz,expires_at=now()+make_interval(secs=>$6::integer)`, key, record.code, record.user.String(), *record.retries, *record.timeout, ttl)
		return userStorageError(err)
	})
}
func (s passwordResetStore) delete(ctx context.Context, key string) error {
	switch s.mode {
	case "cassandra", "migration-to-postgresql":
		if err := s.databases.cassandra.Query(`DELETE FROM password_reset WHERE key=?`, key).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5}).Exec(); err != nil {
			return userStorageError(err)
		}
		if s.mode == "cassandra" {
			return nil
		}
	case "postgresql":
	default:
		return errors.New("invalid password reset storage mode")
	}
	return s.databases.withPostgres(ctx, func(connection *pgx.Conn) error {
		_, err := connection.Exec(ctx, `DELETE FROM password_reset WHERE key=$1`, key)
		return userStorageError(err)
	})
}
func (s passwordResetStore) deleteForUser(ctx context.Context, user gocql.UUID) error {
	return s.delete(ctx, passwordResetKey(user))
}
