// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"

	"github.com/apache/cassandra-gocql-driver/v2"
)

var errRegistrationBlacklisted = errors.New("registration identity blocked")

type registrationIdentity struct {
	owner       func(context.Context, string) (*gocql.UUID, error)
	account     func(context.Context, gocql.UUID) (*storedUser, error)
	deleteKey   func(context.Context, string, *gocql.UUID) error
	blacklisted func(context.Context, string) (bool, error)
}

func (s userAuthStore) registrationEmailOwner(ctx context.Context, email string) (*gocql.UUID, error) {
	key, err := emailUniqueKey(email)
	if err != nil {
		return nil, err
	}
	return s.cassandraLoginLookup(ctx, `SELECT user FROM user_keys WHERE key=?`, key)
}
func (s registrationIdentity) verify(ctx context.Context, email string) error {
	if s.owner == nil || s.account == nil || s.deleteKey == nil || s.blacklisted == nil {
		return errors.New("registration identity dependencies unavailable")
	}
	owner, err := s.owner(ctx, email)
	if err != nil {
		return err
	}
	if owner != nil {
		account, err := s.account(ctx, *owner)
		if err != nil {
			return err
		}
		deleted := account == nil || account.Status != nil && *account.Status == 2
		identity := account != nil && account.Activated && (account.Email != nil || len(account.SSO) > 0)
		switch {
		case deleted:
			// Preserve the native stale-key repair, including its owner guard. It is
			// part of registration behavior and occurs before the block-list check.
			if err := s.deleteKey(ctx, email, owner); err != nil {
				return err
			}
		case identity:
			return errActivationKeyExists
		}
	}
	blocked, err := s.blacklisted(ctx, email)
	if err != nil {
		return err
	}
	if blocked {
		return errRegistrationBlacklisted
	}
	return nil
}
