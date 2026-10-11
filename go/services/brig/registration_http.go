// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"strings"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type registrationHTTP struct {
	service       registrationService
	peers         brigPeers
	galleyVersion int
	activation    *activationHTTP
	invitations   registrationInvitations
	index         *searchIndexWriter
}

func registrationHTTPError(w http.ResponseWriter, err error) {
	var rate passwordRateExceeded
	var admission registrationAdmissionError
	switch {
	case errors.Is(err, errSESMailInvalidDomain):
		authenticationWireError(w, 400, "invalid-email", "Invalid e-mail address.")
	case errors.Is(err, errRegistrationRestricted):
		authenticationWireError(w, 403, "user-creation-restricted", "This instance does not allow creation of personal users or teams.")
	case errors.Is(err, errRegistrationEphemeralDisabled):
		authenticationWireError(w, 403, "ephemeral-user-creation-disabled", "Ephemeral user creation is disabled on this instance.")
	case errors.Is(err, errRegistrationNotAllowlisted):
		authenticationWireError(w, 403, "unauthorized", "Unauthorized e-mail address")
	case errors.As(err, &admission):
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(admission.Code)
		_ = json.NewEncoder(w).Encode(admission)
	case errors.Is(err, errRegistrationMissingIdentity):
		authenticationWireError(w, 403, "missing-identity", "Using an invitation code requires registering the given email.")
	case errors.Is(err, errRegistrationInvalidInvitation):
		authenticationWireError(w, 400, "invalid-invitation-code", "Invalid invitation code.")
	case errors.Is(err, errRegistrationSCIMName):
		authenticationWireError(w, 403, "managed-by-scim", "Updating name is not allowed, because it is managed by SCIM, or E2EId is enabled")
	case errors.Is(err, errInviteNotAllowed):
		authenticationWireError(w, 403, "condition-failed", "`teamInvite` is set to `not-allowed`")
	case errors.Is(err, errInviteOtherTeam):
		authenticationWireError(w, 403, "condition-failed", "`teamInvite` is restricted to another team.")
	case errors.Is(err, errRegistrationTooManyTeamMembers):
		authenticationWireError(w, 403, "too-many-team-members", "Too many members in this team.")
	case errors.Is(err, errRegistrationBlacklisted):
		authenticationWireError(w, 403, "blacklisted-email", "The given e-mail address has been blacklisted due to a permanent bounce or a complaint.")
	case errors.As(err, &rate):
		authenticationHTTPError(w, err)
	default:
		activationHTTPError(w, err)
	}
}
func (h registrationHTTP) handleInternal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(media, "application/json") {
		w.WriteHeader(http.StatusUnsupportedMediaType)
		return
	}
	request, err := decodeRegistration(r.Body, false)
	if err != nil {
		wireError(w, 400, "bad-request", "Invalid registration request")
		return
	}
	if h.activation == nil {
		registrationHTTPError(w, errors.New("registration activation unavailable"))
		return
	}
	requestID := r.Header.Get("Request-Id")
	service := h.service
	if request.Origin.TeamCode != nil {
		resolver := h.invitations
		resolver.checkJoin = func(ctx context.Context, team gocql.UUID) error {
			return h.peers.checkRegistrationJoin(ctx, team, requestID)
		}
		resolver.size = func(ctx context.Context, team gocql.UUID) (teamSize, error) {
			return h.index.teamSize(ctx, team, requestID)
		}
		service.findInvitation = resolver.find
		service.joinInvitation = func(ctx context.Context, user gocql.UUID, inv storedInvitation) (bool, error) {
			role := int32(3)
			if inv.Role != nil {
				role = *inv.Role
			}
			return h.peers.joinRegistrationMember(ctx, user, inv.Team, &inv, role, requestID)
		}
	}
	service.createSelf = func(ctx context.Context, user gocql.UUID) error {
		return h.peers.createSelfConversation(ctx, user, h.galleyVersion, requestID)
	}
	service.createTeam = func(ctx context.Context, user, team gocql.UUID, input registrationTeam) error {
		return h.peers.createRegistrationTeam(ctx, user, team, input, requestID)
	}
	service.joinSSO = func(ctx context.Context, user, team gocql.UUID) (bool, error) {
		return h.peers.joinRegistrationSSO(ctx, user, team, requestID)
	}
	service.teamName = func(ctx context.Context, team gocql.UUID) (string, error) {
		return h.peers.registrationTeamName(ctx, team, requestID)
	}
	service.onAccountActivated = func(ctx context.Context, result *activatedIdentity) error {
		api := h.activation.forRequest(requestID, nil)
		if err := api.syncIndex(ctx, result.user); err != nil {
			return err
		}
		return api.generateEvent(ctx, result)
	}
	service.activate = func(ctx context.Context, key, code string, override *gocql.UUID, currency *string) error {
		_, err := h.activation.forRequest(requestID, currency).run(ctx, key, code, false, true, override)
		return err
	}
	profile, err := service.createInternal(r.Context(), request)
	if err != nil {
		registrationHTTPError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json;charset=utf-8")
	w.Header().Set("Location", profile.ID)
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(profile)
}
