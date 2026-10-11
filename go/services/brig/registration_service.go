// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type registrationCreation struct {
	profile    selfProfile
	activation *activationRecord
}

func (registrationCreation) String() string { return "<RegistrationCreation>" }

type registrationService struct {
	identity          registrationIdentity
	guardDomain       func(context.Context, string) error
	hash              func(context.Context, passwordRateKey, string, int) (string, error)
	createAccount     func(context.Context, storedUser) error
	createSelf        func(context.Context, gocql.UUID) error
	createTeam        func(context.Context, gocql.UUID, gocql.UUID, registrationTeam) error
	newCode           func(context.Context, string, float64, *gocql.UUID) (activationRecord, error)
	activate          func(context.Context, string, string, *gocql.UUID, *string) error
	initFeature       func(context.Context, gocql.UUID) error
	domain            string
	locale            registrationLocale
	sessionTTL        int64
	activationTimeout float64
	now               func() time.Time
}

// Native createUserWith order for independent accounts and team creators.
// Invitation/SSO origin resolution must be implemented before those flows can
// reach account storage. Missing flows fail explicitly, never succeed partially.
func (s registrationService) create(ctx context.Context, request registrationRequest, rate passwordRateKey) (registrationCreation, error) {
	var result registrationCreation
	if s.guardDomain == nil || s.hash == nil || s.createAccount == nil || s.createSelf == nil || s.newCode == nil || s.activate == nil || s.initFeature == nil || s.now == nil {
		return result, errors.New("registration dependencies unavailable")
	}
	n := request.User
	if n.Email != nil {
		if err := s.identity.verify(ctx, *n.Email); err != nil {
			return result, err
		}
	}
	// The historical non-team invitation_code is parsed and origin-validated,
	// but createUserWith derives team behavior only from newUserTeam.
	if request.Origin.TeamCode != nil || request.Origin.SSOTeam != nil {
		return result, errors.New("registration invitation or SSO origin unavailable")
	}
	if n.Email != nil {
		if err := s.guardDomain(ctx, *n.Email); err != nil {
			return result, err
		}
	}
	var team *gocql.UUID
	if request.Origin.Team != nil {
		if s.createTeam == nil {
			return result, errors.New("registration team creation unavailable")
		}
		id, err := gocql.RandomUUID()
		if err != nil {
			return result, errors.New("registration randomness unavailable")
		}
		team = &id
	}
	var password *string
	if request.Password != nil {
		encoded, err := s.hash(ctx, rate, *request.Password, 8)
		if err != nil {
			return result, err
		}
		password = &encoded
	}
	account, err := n.build(nil, team, password, s.locale, s.sessionTTL, s.now())
	if err != nil {
		return result, err
	}
	profile, err := registrationProfile(account, s.domain, s.locale.Language)
	if err != nil {
		return result, err
	}
	if err := s.createAccount(ctx, account); err != nil {
		return result, err
	}
	if err := s.createSelf(ctx, account.ID); err != nil {
		return result, err
	}
	// Native UserCreated sends neither notification nor journal entry.
	if team != nil {
		if err := s.createTeam(ctx, account.ID, *team, *request.Origin.Team); err != nil {
			return result, err
		}
	}
	if n.Email != nil {
		if request.EmailCode == nil {
			code, err := s.newCode(ctx, *n.Email, s.activationTimeout, &account.ID)
			if err != nil {
				return result, err
			}
			result.activation = &code
		} else {
			key, err := activationEmailKey(*n.Email)
			if err != nil {
				return result, err
			}
			var currency *string
			if request.Origin.Team != nil {
				currency = request.Origin.Team.Currency
			}
			if err := s.activate(ctx, key, *request.EmailCode, &account.ID, currency); err != nil {
				return registrationCreation{}, err
			}
		}
	}
	if err := s.initFeature(ctx, account.ID); err != nil {
		return registrationCreation{}, err
	}
	result.profile = profile
	return result, nil
}

func (s registrationService) createInternal(ctx context.Context, request registrationRequest) (selfProfile, error) {
	result, err := s.create(ctx, request, passwordRateKey{kind: "internal"})
	if err != nil {
		return selfProfile{}, err
	}
	if code := result.activation; code != nil {
		user, err := registrationUUID(result.profile.ID)
		if err != nil {
			return selfProfile{}, err
		}
		if err := s.activate(ctx, code.key, code.code, &user, nil); err != nil {
			return selfProfile{}, err
		}
	}
	// Native createUserNoVerify returns the creation snapshot, not a reread of
	// activated state. This distinction matters for pending/unvalidated identity.
	return result.profile, nil
}
