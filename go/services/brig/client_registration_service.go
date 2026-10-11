// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"

	"github.com/apache/cassandra-gocql-driver/v2"
)

var (
	errClientUserMissing           = errors.New("invalid client user")
	errClientLegalHoldRegistration = errors.New("legalhold client cannot be added manually")
	errClientCodeRequired          = errors.New("client verification code required")
	errClientCodeFailed            = errors.New("client verification code failed")
)

type clientRegistrationService struct {
	core                clientRegistrationCore
	account             func(context.Context, gocql.UUID) (*storedUser, error)
	verificationEnabled func(context.Context, string, string) (bool, error)
	verifyCode          func(context.Context, string, string, string) (bool, error)
	consumable          bool
	setup               func(context.Context, gocql.UUID, string, string) error
	deleteOld           func(context.Context, gocql.UUID, *string, storedClient) error
	galley              func(context.Context, gocql.UUID, string, string) error
	event               func(context.Context, gocql.UUID, *string, storedClient, string) error
	mail                func(context.Context, selfProfile, storedClient) error
	domain, locale      string
}

func (s clientRegistrationService) readAccount(ctx context.Context, user gocql.UUID) (*storedUser, *selfProfile, error) {
	row, err := s.account(ctx, user)
	if err != nil || row == nil {
		return row, nil, err
	}
	// Native getAccountNoFilter includes missing identity, every status and
	// expired invitations; do not use the filtered public lookup here.
	profile, err := row.selfProfile(s.domain, s.locale)
	return row, &profile, err
}

func (s clientRegistrationService) addPublic(ctx context.Context, user gocql.UUID, connection string, request clientRegistrationRequest, requestID string) (storedClient, error) {
	var zero storedClient
	if request.client.Type == 2 {
		return zero, errClientLegalHoldRegistration
	}
	_, profile, err := s.readAccount(ctx, user)
	if err != nil {
		return zero, err
	}
	if profile == nil {
		return zero, errClientUserMissing
	}
	// Native verification performs separate account lookups for email/team and
	// SAML after the initial account lookup, even when the feature is disabled.
	_, codeAccount, err := s.readAccount(ctx, user)
	if err != nil {
		return zero, err
	}
	required := false
	if codeAccount != nil && codeAccount.Team != nil {
		required, err = s.verificationEnabled(ctx, *codeAccount.Team, requestID)
		if err != nil {
			return zero, err
		}
	}
	samlRow, _, err := s.readAccount(ctx, user)
	if err != nil {
		return zero, err
	}
	saml, err := storedSAML(samlRow)
	if err != nil {
		return zero, err
	}
	if required && !saml {
		if request.verificationCode == nil {
			return zero, errClientCodeRequired
		}
		if codeAccount == nil || codeAccount.Email == nil {
			return zero, errClientCodeFailed
		}
		valid, err := s.verifyCode(ctx, *codeAccount.Email, "login", *request.verificationCode)
		if err != nil {
			return zero, err
		}
		if !valid {
			return zero, errClientCodeFailed
		}
	}
	client, old, total, err := s.core.upsert(ctx, user, request)
	if err != nil {
		return zero, err
	}
	for _, cap := range client.Capabilities {
		if s.consumable && cap == 2 {
			if err := s.setup(ctx, user, client.ID, requestID); err != nil {
				return zero, err
			}
			break
		}
	}
	for _, candidate := range old {
		if err := s.deleteOld(ctx, user, &connection, candidate); err != nil {
			return zero, err
		}
	}
	if err := s.galley(ctx, user, client.ID, requestID); err != nil {
		return zero, err
	}
	if err := s.event(ctx, user, &connection, client, requestID); err != nil {
		return zero, err
	}
	if total > 1 && profile.Email != nil {
		if err := s.mail(ctx, *profile, client); err != nil {
			return zero, err
		}
	}
	return client, nil
}
