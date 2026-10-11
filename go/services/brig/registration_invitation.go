// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"
	"math/big"
	"strings"

	"github.com/apache/cassandra-gocql-driver/v2"
)

var (
	errRegistrationMissingIdentity   = errors.New("invitation registration requires email")
	errRegistrationInvalidInvitation = errors.New("invalid registration invitation")
	errRegistrationSCIMName          = errors.New("registration name managed by SCIM")
	errInviteNotAllowed              = errors.New("domain team invitations not allowed")
	errInviteOtherTeam               = errors.New("domain team invitations restricted to another team")
)

type registrationInvitations struct {
	lookup    func(context.Context, string) (*storedInvitation, error)
	domain    func(context.Context, string) (*domainRegistration, error)
	size      func(context.Context, gocql.UUID) (teamSize, error)
	checkJoin func(context.Context, gocql.UUID) error
	maxSize   uint32
}

func (s registrationInvitations) find(ctx context.Context, email *string, code string) (*storedInvitation, error) {
	if email == nil {
		return nil, errRegistrationMissingIdentity
	}
	if s.lookup == nil || s.domain == nil || s.size == nil || s.checkJoin == nil {
		return nil, errors.New("registration invitation dependencies unavailable")
	}
	inv, err := s.lookup(ctx, code)
	if err != nil {
		return nil, err
	}
	if inv == nil {
		return nil, errRegistrationInvalidInvitation
	}
	key, err := emailUniqueKey(*email)
	if err != nil {
		return nil, err
	}
	inviteKey, err := emailUniqueKey(inv.Email)
	if err != nil {
		return nil, err
	}
	if key != inviteKey {
		return nil, errRegistrationInvalidInvitation
	}
	// Domain guards use the registering address's original domain, rather
	// than adding an ordinary domain-redirection restriction to invitations.
	at := strings.LastIndexByte(*email, '@')
	if at < 0 {
		return nil, errInvalidEmailDomain
	}
	domain, err := parseEmailDomain((*email)[at+1:])
	if err != nil {
		return nil, err
	}
	reg, err := s.domain(ctx, domain)
	if err != nil {
		return nil, err
	}
	if reg != nil && reg.invite != nil {
		switch *reg.invite {
		case 2:
			return nil, errInviteNotAllowed
		case 3:
			if reg.team == nil || *reg.team != inv.Team {
				return nil, errInviteOtherTeam
			}
		}
	}
	size, err := s.size(ctx, inv.Team)
	if err != nil {
		return nil, err
	}
	if size.Total == nil {
		return nil, errors.New("invalid team size")
	}
	if size.Total.Cmp(new(big.Int).SetUint64(uint64(s.maxSize))) >= 0 {
		return nil, errRegistrationTooManyTeamMembers
	}
	if err := s.checkJoin(ctx, inv.Team); err != nil {
		return nil, err
	}
	return inv, nil
}
