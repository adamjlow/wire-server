// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type registrationCreation struct {
	profile    selfProfile
	activation *activationRecord
	team       *registrationCreatedTeam
}

type registrationCreatedTeam struct {
	ID   gocql.UUID
	Name string
}

var errRegistrationTooManyTeamMembers = errors.New("registration team member limit reached")

func (registrationCreation) String() string { return "<RegistrationCreation>" }

type registrationService struct {
	preserveSCIMDisplayName bool
	identity                registrationIdentity
	findInvitation          func(context.Context, *string, string) (*storedInvitation, error)
	invitedAccount          func(context.Context, gocql.UUID) (*selfProfile, error)
	claimInvitationKey      func(context.Context, string, gocql.UUID) (bool, error)
	joinInvitation          func(context.Context, gocql.UUID, storedInvitation) (bool, error)
	removePending           func(context.Context, gocql.UUID) error
	deleteInvitation        func(context.Context, gocql.UUID, gocql.UUID) error
	guardDomain             func(context.Context, string) error
	hash                    func(context.Context, passwordRateKey, string, int) (string, error)
	createAccount           func(context.Context, storedUser) error
	createSelf              func(context.Context, gocql.UUID) error
	createTeam              func(context.Context, gocql.UUID, gocql.UUID, registrationTeam) error
	joinSSO                 func(context.Context, gocql.UUID, gocql.UUID) (bool, error)
	activateStored          func(context.Context, gocql.UUID, *string, bool) error
	onAccountActivated      func(context.Context, *activatedIdentity) error
	teamName                func(context.Context, gocql.UUID) (string, error)
	newCode                 func(context.Context, string, float64, *gocql.UUID) (activationRecord, error)
	activate                func(context.Context, string, string, *gocql.UUID, *string) error
	initFeature             func(context.Context, gocql.UUID) error
	domain                  string
	locale                  registrationLocale
	sessionTTL              int64
	activationTimeout       float64
	now                     func() time.Time
}

// Preserve createUserWith's validation, persistence and partial-failure order.
func (s registrationService) create(ctx context.Context, request registrationRequest, rate passwordRateKey) (result registrationCreation, err error) {
	stage := "creation-dependencies"
	defer func() {
		if err != nil {
			err = registrationFailure{stage: stage, cause: err}
		}
	}()
	if s.guardDomain == nil || s.hash == nil || s.createAccount == nil || s.createSelf == nil || s.newCode == nil || s.activate == nil || s.initFeature == nil || s.now == nil {
		return result, errors.New("registration dependencies unavailable")
	}
	stage = "identity"
	n := request.User
	if n.Email != nil {
		if err := s.identity.verify(ctx, *n.Email); err != nil {
			return result, err
		}
	}
	// The historical non-team invitation_code is parsed and origin-validated,
	// but createUserWith derives team behavior only from newUserTeam.
	stage = "invitation"
	var invitation *storedInvitation
	var invitationID *gocql.UUID
	var handle *string
	team := request.Origin.SSOTeam
	if request.Origin.TeamCode != nil {
		if s.findInvitation == nil || s.invitedAccount == nil || s.claimInvitationKey == nil || s.joinInvitation == nil || s.activateStored == nil || s.onAccountActivated == nil || s.removePending == nil || s.deleteInvitation == nil || s.teamName == nil {
			return result, errors.New("registration invitation dependencies unavailable")
		}
		var err error
		invitation, err = s.findInvitation(ctx, n.Email, *request.Origin.TeamCode)
		if err != nil {
			return result, err
		}
		if invitation == nil {
			return result, errRegistrationInvalidInvitation
		}
		team, invitationID = &invitation.Team, &invitation.ID
		existing, err := s.invitedAccount(ctx, invitation.ID)
		if err != nil {
			return result, err
		}
		if existing != nil {
			handle = existing.Handle
			managed := int32(0)
			if existing.ManagedBy == "scim" {
				if s.preserveSCIMDisplayName {
					n.Name = existing.Name
				} else if n.Name != existing.Name {
					return result, errRegistrationSCIMName
				}
				managed = 1
				var identity struct {
					External *string `json:"scim_external_id"`
				}
				if len(existing.SSO) > 0 && json.Unmarshal(existing.SSO, &identity) != nil {
					return result, errors.New("invalid provisioned invitation identity")
				}
				if identity.External != nil {
					n.SSO = existing.SSO
				} else if n.Email != nil {
					n.SSO, _ = json.Marshal(map[string]string{"scim_external_id": *n.Email})
				}
			}
			n.ManagedBy = &managed
		}
	}
	stage = "domain"
	if n.Email != nil && request.Origin.SSOTeam == nil && invitation == nil {
		if err := s.guardDomain(ctx, *n.Email); err != nil {
			return result, err
		}
	}
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
	stage = "password"
	var password *string
	if request.Password != nil {
		encoded, err := s.hash(ctx, rate, *request.Password, 8)
		if err != nil {
			return result, err
		}
		password = &encoded
	}
	stage = "account-construction"
	account, err := n.build(invitationID, team, password, s.locale, s.sessionTTL, s.now())
	if err != nil {
		return result, err
	}
	profile, err := registrationProfile(account, s.domain, s.locale.Language)
	if err != nil {
		return result, err
	}
	profile.Handle = handle
	activationView := account
	activationView.Activated = true
	joinSSO, err := storedSAML(&activationView)
	if err != nil {
		return result, err
	}
	joinSSO = joinSSO && request.Origin.SSOTeam != nil
	if joinSSO && (s.joinSSO == nil || s.activateStored == nil || s.onAccountActivated == nil || s.teamName == nil) {
		return result, errors.New("registration SSO dependencies unavailable")
	}
	stage = "account-write"
	if err := s.createAccount(ctx, account); err != nil {
		return result, err
	}
	stage = "self-conversation"
	if err := s.createSelf(ctx, account.ID); err != nil {
		return result, err
	}
	// Native UserCreated sends neither notification nor journal entry.
	if request.Origin.Team != nil {
		if err := s.createTeam(ctx, account.ID, *team, *request.Origin.Team); err != nil {
			return result, err
		}
		if request.EmailCode != nil {
			result.team = &registrationCreatedTeam{ID: *team, Name: request.Origin.Team.Name}
		}
	}
	if invitation != nil {
		claimed, err := s.claimInvitationKey(ctx, invitation.Email, account.ID)
		if err != nil {
			return result, err
		}
		if !claimed {
			return result, errActivationKeyExists
		}
		added, err := s.joinInvitation(ctx, account.ID, *invitation)
		if err != nil {
			return result, err
		}
		if !added {
			return result, errRegistrationTooManyTeamMembers
		}
		if err := s.activateStored(ctx, account.ID, &invitation.Email, true); err != nil {
			return result, err
		}
		if err := s.onAccountActivated(ctx, &activatedIdentity{user: account.ID, first: true, account: &activationView, creationProfile: &profile}); err != nil {
			return result, err
		}
		if err := s.removePending(ctx, account.ID); err != nil {
			return result, err
		}
		if err := s.deleteInvitation(ctx, invitation.Team, invitation.ID); err != nil {
			return result, err
		}
		name, err := s.teamName(ctx, invitation.Team)
		if err != nil {
			return result, err
		}
		result.team = &registrationCreatedTeam{ID: invitation.Team, Name: name}
	}
	if joinSSO {
		added, err := s.joinSSO(ctx, account.ID, *team)
		if err != nil {
			return result, err
		}
		if !added {
			return result, errRegistrationTooManyTeamMembers
		}
		if err := s.activateStored(ctx, account.ID, n.Email, true); err != nil {
			return result, err
		}
		if err := s.onAccountActivated(ctx, &activatedIdentity{user: account.ID, first: true, account: &activationView, creationProfile: &profile}); err != nil {
			return result, err
		}
		name, err := s.teamName(ctx, *team)
		if err != nil {
			return result, err
		}
		result.team = &registrationCreatedTeam{ID: *team, Name: name}
	}
	stage = "activation-code"
	if n.Email != nil && invitation == nil {
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
	stage = "feature"
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
