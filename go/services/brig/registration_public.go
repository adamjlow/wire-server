// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"
	"net/netip"
	"slices"
	"strings"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

var (
	errRegistrationRestricted        = errors.New("personal registration restricted")
	errRegistrationEphemeralDisabled = errors.New("ephemeral registration disabled")
	errRegistrationNotAllowlisted    = errors.New("registration email not allowlisted")
)

type registrationPolicy struct {
	restrict     bool
	ephemeral    bool
	allowDomains *[]string
}

func newRegistrationPolicy(c Config) (registrationPolicy, error) {
	if c.Settings.EphemeralCreation == nil {
		return registrationPolicy{}, errors.New("missing native ephemeral registration option")
	}
	policy := registrationPolicy{ephemeral: *c.Settings.EphemeralCreation, allowDomains: c.Settings.AllowlistDomains}
	if c.Settings.RestrictUserCreation != nil {
		policy.restrict = *c.Settings.RestrictUserCreation
	}
	return policy, nil
}

func (p registrationPolicy) check(request registrationRequest) error {
	user := request.User
	ephemeral := user.Email == nil && len(user.SSO) == 0 && (user.ManagedBy == nil || *user.ManagedBy == 0)
	member := request.Origin.TeamCode != nil || request.Origin.SSOTeam != nil
	if p.restrict && !member && !ephemeral {
		return errRegistrationRestricted
	}
	if !p.ephemeral && ephemeral {
		return errRegistrationEphemeralDisabled
	}
	if user.Email != nil && p.allowDomains != nil {
		at := strings.LastIndexByte(*user.Email, '@')
		// Native allowlist compares domainPart text exactly, without wildcard,
		// suffix matching or additional lowercasing of the configured values.
		if at < 0 || !slices.Contains(*p.allowDomains, (*user.Email)[at+1:]) {
			return errRegistrationNotAllowlisted
		}
	}
	return nil
}

type publicRegistration struct {
	policy         registrationPolicy
	create         func(context.Context, registrationRequest, passwordRateKey) (registrationCreation, error)
	activationMail func(context.Context, registrationCreation, registrationOrigin) error
	welcomeMail    func(context.Context, selfProfile, registrationCreatedTeam) error
	cookie         func(context.Context, gocql.UUID, int32, *string, time.Time) (issuedCookie, error)
	now            func() time.Time
}

type publicRegistrationResult struct {
	profile selfProfile
	cookie  issuedCookie
}

func newPublicRegistration(policy registrationPolicy, create func(context.Context, registrationRequest, passwordRateKey) (registrationCreation, error), activationMail func(context.Context, registrationCreation, registrationOrigin) error, welcomeMail func(context.Context, selfProfile, registrationCreatedTeam) error, sessions sessionManager, now func() time.Time) publicRegistration {
	return publicRegistration{policy: policy, create: create, activationMail: activationMail, welcomeMail: welcomeMail, now: now,
		cookie: func(ctx context.Context, user gocql.UUID, typ int32, label *string, at time.Time) (issuedCookie, error) {
			// Native registration calls newCookie, not limitedCookie (login).
			// It issues U cookies with no client and revokes the same label.
			return sessions.newCookie(ctx, user, nil, typ, label, false, true, at)
		},
	}
}

func (publicRegistrationResult) String() string { return "<PublicRegistrationResult>" }

func (s publicRegistration) register(ctx context.Context, request registrationRequest, ip netip.Addr) (publicRegistrationResult, error) {
	var result publicRegistrationResult
	if err := s.policy.check(request); err != nil {
		return result, err
	}
	if s.create == nil || s.activationMail == nil || s.welcomeMail == nil || s.cookie == nil || s.now == nil {
		return result, errors.New("public registration dependencies unavailable")
	}
	if !ip.IsValid() {
		return result, errors.New("registration IP unavailable")
	}
	created, err := s.create(ctx, request, passwordRateKey{kind: "ip", ip: ip})
	if err != nil {
		return result, err
	}
	if created.profile.Email != nil {
		if created.activation != nil {
			if err := s.activationMail(ctx, created, request.Origin); err != nil {
				return result, registrationFailure{stage: "activation-mail", cause: err}
			}
		}
		if created.team != nil && (request.Origin.TeamCode != nil || request.Origin.SSOTeam != nil) {
			if err := s.welcomeMail(ctx, created.profile, *created.team); err != nil {
				return result, registrationFailure{stage: "welcome-mail", cause: err}
			}
		}
	}
	user, err := registrationUUID(created.profile.ID)
	if err != nil {
		return result, errors.New("invalid registration account")
	}
	typ := int32(1)
	if created.profile.Status == "ephemeral" {
		typ = 0
	}
	cookie, err := s.cookie(ctx, user, typ, request.Label, s.now())
	if err != nil {
		return result, registrationFailure{stage: "cookie", cause: err}
	}
	return publicRegistrationResult{profile: created.profile, cookie: cookie}, nil
}

// Private stage-only diagnostics retain typed HTTP errors via Unwrap without
// logging addresses, tokens, passwords, request bodies or driver error details.
type registrationFailure struct {
	stage string
	cause error
}

func (e registrationFailure) Error() string { return "registration failed at " + e.stage }
func (e registrationFailure) Unwrap() error { return e.cause }
