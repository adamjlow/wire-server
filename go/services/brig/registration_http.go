// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"mime"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type registrationHTTP struct {
	policy         registrationPolicy
	mail           *registrationMailDelivery
	sessions       sessionManager
	cookieInsecure bool
	service        registrationService
	peers          brigPeers
	galleyVersion  int
	activation     *activationHTTP
	invitations    registrationInvitations
	index          *searchIndexWriter
}

func registrationHTTPError(w http.ResponseWriter, err error) {
	var rate passwordRateExceeded
	var admission registrationAdmissionError
	var failure registrationFailure
	if errors.As(err, &failure) {
		log.Print("registration failed at ", failure.stage)
	}
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
	service, err := h.forRequest(request, r.Header.Get("Request-Id"))
	if err != nil {
		registrationHTTPError(w, err)
		return
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

func (h registrationHTTP) forRequest(request registrationRequest, requestID string) (registrationService, error) {
	if h.activation == nil {
		return registrationService{}, errors.New("registration activation unavailable")
	}
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
	return service, nil
}

func (h registrationHTTP) handlePublic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	values, present := r.Header["X-Forwarded-For"]
	if !present || len(values) == 0 {
		routeParseError(w, "Header X-Forwarded-For is required")
		return
	}
	ip, err := netip.ParseAddr(values[0])
	if err != nil || ip.Zone() != "" {
		routeParseError(w, "Error parsing header X-Forwarded-For failed: Failed reading: Failed parsing bytestring as IpAddr.")
		return
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(media, "application/json") {
		w.WriteHeader(http.StatusUnsupportedMediaType)
		return
	}
	request, err := decodeRegistration(r.Body, true)
	if err != nil {
		wireError(w, 400, "bad-request", "Invalid registration request")
		return
	}
	if err = h.policy.check(request); err != nil {
		registrationHTTPError(w, err)
		return
	}
	service, err := h.forRequest(request, r.Header.Get("Request-Id"))
	if err != nil {
		registrationHTTPError(w, err)
		return
	}
	version, _ := strconv.Atoi(r.Header.Get("X-Wire-API-Version"))
	service.preserveSCIMDisplayName = version < 18
	registration := newPublicRegistration(h.policy, service.create, h.mail.activation, h.mail.welcome, h.sessions, time.Now)
	result, err := registration.register(r.Context(), request, ip)
	if err != nil {
		registrationHTTPError(w, err)
		return
	}
	value, err := result.cookie.token.Encode()
	if err != nil {
		registrationHTTPError(w, err)
		return
	}
	cookie := http.Cookie{Name: "zuid", Value: value, Path: "/access", HttpOnly: true, Secure: !h.cookieInsecure}
	if result.cookie.record.Type == 1 {
		cookie.Expires = result.cookie.record.Expires
	}
	http.SetCookie(w, &cookie)
	w.Header().Set("Content-Type", "application/json;charset=utf-8")
	w.Header().Set("Location", result.profile.ID)
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(result.profile)
}
