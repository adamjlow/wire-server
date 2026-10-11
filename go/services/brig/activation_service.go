// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"

	"github.com/apache/cassandra-gocql-driver/v2"
)

var (
	errActivationWrongUser = errors.New("activation user unavailable")
	errActivationKeyExists = errors.New("activation identity already claimed")
)

type activationCodeLookup interface {
	verify(context.Context, string, string) (*activationRecord, error)
}
type activationIdentities interface {
	claimEmailKey(context.Context, string, gocql.UUID) (bool, error)
	setEmailIdentity(context.Context, gocql.UUID, *string, bool) error
	clearUnvalidatedEmail(context.Context, gocql.UUID) error
	deleteEmailKey(context.Context, string, *gocql.UUID) error
}
type activationService struct {
	codes               activationCodeLookup
	account             func(context.Context, gocql.UUID) (*storedUser, error)
	identities          activationIdentities
	deletePasswordReset func(context.Context, gocql.UUID) error
}
type activatedIdentity struct {
	user    gocql.UUID
	email   string
	first   bool
	account *storedUser
}

func (activatedIdentity) String() string { return "<ActivatedIdentity>" }

// Mirrors Brig.Data.Activation.activateKey. Domain preverification, index,
// notifications/journal and team activation are separate native API steps.
func (s activationService) activateKey(ctx context.Context, key, code string, override *gocql.UUID) (*activatedIdentity, error) {
	record, err := s.codes.verify(ctx, key, code)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, errInvalidActivationCode
	}
	user := record.user
	if override != nil {
		user = override
	}
	if user == nil {
		return nil, errActivationWrongUser
	}
	account, err := s.account(ctx, *user)
	if err != nil {
		return nil, err
	}
	if account == nil {
		return nil, errActivationWrongUser
	}
	status := int32(0)
	if account.Status != nil {
		status = *account.Status
	}
	if status != 0 {
		return nil, errInvalidActivationCode
	}
	claim := func() error {
		claimed, err := s.identities.claimEmailKey(ctx, record.email, *user)
		if err != nil {
			return err
		}
		if !claimed {
			return errActivationKeyExists
		}
		return nil
	}
	// Native userIdentity hides all stored identity until activated; an active
	// account with neither email nor SSO likewise has no identity.
	if !account.Activated || account.Email == nil && len(account.SSO) == 0 {
		if err := claim(); err != nil {
			return nil, err
		}
		if err := s.identities.setEmailIdentity(ctx, *user, &record.email, true); err != nil {
			return nil, err
		}
		activated := *account
		activated.Activated = true
		activated.Email = &record.email
		activated.SSO = nil
		return &activatedIdentity{user: *user, email: record.email, first: true, account: &activated}, nil
	}
	newKey, err := emailUniqueKey(record.email)
	if err != nil {
		return nil, err
	}
	var oldKey *string
	if account.Email != nil {
		key, err := emailUniqueKey(*account.Email)
		if err != nil {
			return nil, err
		}
		oldKey = &key
	}
	sameKey := oldKey != nil && *oldKey == newKey
	if sameKey && *account.Email == record.email {
		return nil, nil
	}
	if !sameKey {
		// Native invalidates the reset code even when the following claim fails.
		if err := s.deletePasswordReset(ctx, *user); err != nil {
			return nil, err
		}
		if err := claim(); err != nil {
			return nil, err
		}
	}
	if err := s.identities.setEmailIdentity(ctx, *user, &record.email, false); err != nil {
		return nil, err
	}
	if err := s.identities.clearUnvalidatedEmail(ctx, *user); err != nil {
		return nil, err
	}
	if !sameKey && account.Email != nil {
		if err := s.identities.deleteEmailKey(ctx, *account.Email, nil); err != nil {
			return nil, err
		}
	}
	return &activatedIdentity{user: *user, email: record.email}, nil
}
