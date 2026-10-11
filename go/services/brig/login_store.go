// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"
	"strings"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/jackc/pgx/v5"
)

// The same case-sensitive trusted-domain rule serves login, activation and
// verification keys. Inputs here have already passed the email API parser.
func emailUniqueKey(email string) (string, error) {
	local, domain, ok := strings.Cut(email, "@")
	if !ok || local == "" || domain == "" {
		return "", errors.New("invalid stored email")
	}
	if domain != "wearezeta.com" && domain != "wire.com" && domain != "simulator.amazonses.com" {
		local, _, _ = strings.Cut(local, "+")
	}
	return strings.ToLower(local) + "@" + strings.ToLower(domain), nil
}

type loginIdentifier struct{ email, handle *string }

func (s userAuthStore) resolveLogin(ctx context.Context, identifier loginIdentifier) (*gocql.UUID, error) {
	if identifier.email != nil {
		key, err := emailUniqueKey(*identifier.email)
		if err != nil {
			return nil, err
		}
		return s.cassandraLoginLookup(ctx, `SELECT user FROM user_keys WHERE key=?`, key)
	}
	if identifier.handle == nil {
		return nil, errors.New("missing login identifier")
	}
	switch s.mode {
	case "postgresql", "migration-to-postgresql":
		var user *gocql.UUID
		err := s.withPostgres(ctx, func(connection *pgx.Conn) error {
			var text string
			err := connection.QueryRow(ctx, `SELECT id::text FROM wire_user WHERE handle=$1`, *identifier.handle).Scan(&text)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			if err != nil {
				return userStorageError(err)
			}
			id, err := gocql.ParseUUID(text)
			if err != nil {
				return errors.New("invalid stored login user")
			}
			user = &id
			return nil
		})
		if err != nil || user != nil || s.mode == "postgresql" {
			return user, err
		}
	case "cassandra":
	default:
		return nil, errors.New("invalid user storage mode")
	}
	// Native migrating handle lookup falls back without a per-user lock: the
	// user is unknown until lookup completes. PostgreSQL takes precedence.
	return s.cassandraLoginLookup(ctx, `SELECT user FROM user_handle WHERE handle=?`, *identifier.handle)
}

func (s userAuthStore) cassandraLoginLookup(ctx context.Context, query, key string) (*gocql.UUID, error) {
	var user *gocql.UUID
	err := s.cassandra.Query(query, key).WithContext(ctx).Consistency(gocql.LocalQuorum).
		RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).Scan(&user)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, nil
	}
	return user, userStorageError(err)
}

func (s userAuthStore) pendingLoginActivation(ctx context.Context, email string) (bool, error) {
	encoded, err := activationEmailKey(email)
	if err != nil {
		return false, err
	}
	var user *gocql.UUID
	var code string
	err = s.cassandra.Query(`SELECT user,code FROM activation_keys WHERE key=?`, encoded).
		WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).Scan(&user, &code)
	if errors.Is(err, gocql.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, userStorageError(err)
	}
	if user == nil {
		return false, nil
	}
	account, err := (userStore{s}).get(ctx, *user)
	if err != nil || account == nil {
		return false, err
	}
	status := int32(0)
	if account.Status != nil {
		status = *account.Status
	}
	if status != 0 && status != 4 {
		return false, nil
	}
	if !account.Activated {
		return true, nil
	}
	if len(account.SSO) != 0 {
		return false, nil
	}
	if account.Email == nil {
		return true, nil
	}
	activated, err := emailUniqueKey(*account.Email)
	if err != nil {
		return false, err
	}
	key, err := emailUniqueKey(email)
	return activated != key, err
}

type loginBudget struct {
	seconds   float64
	remaining int32
}
type loginBudgetStorage interface {
	lookup(context.Context, string) (*loginBudget, error)
	insert(context.Context, string, loginBudget) error
}
type loginBudgetStore struct{ cassandra *gocql.Session }

func (s loginBudgetStore) lookup(ctx context.Context, key string) (*loginBudget, error) {
	var value, ttl int32
	err := s.cassandra.Query(`SELECT budget,ttl(budget) FROM budget WHERE key=?`, key).
		WithContext(ctx).Consistency(gocql.One).Scan(&value, &ttl)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.New("login budget lookup failed")
	}
	return &loginBudget{seconds: float64(ttl), remaining: value}, nil
}
func (s loginBudgetStore) insert(ctx context.Context, key string, budget loginBudget) error {
	ttl, err := cassandraTTL(budget.seconds)
	if err != nil {
		return err
	}
	err = s.cassandra.Query(`INSERT INTO budget (key,budget) VALUES (?,?) USING TTL ?`, key, budget.remaining, ttl).
		WithContext(ctx).Consistency(gocql.One).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5}).Exec()
	if err != nil {
		return errors.New("login budget update failed")
	}
	return nil
}
