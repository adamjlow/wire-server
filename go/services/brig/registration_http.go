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
}

func registrationHTTPError(w http.ResponseWriter, err error) {
	var rate passwordRateExceeded
	switch {
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
	service.createSelf = func(ctx context.Context, user gocql.UUID) error {
		return h.peers.createSelfConversation(ctx, user, h.galleyVersion, requestID)
	}
	service.createTeam = func(ctx context.Context, user, team gocql.UUID, input registrationTeam) error {
		return h.peers.createRegistrationTeam(ctx, user, team, input, requestID)
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
