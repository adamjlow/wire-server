// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/mail"
	"unicode/utf8"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type activationHTTP struct {
	service    activationAPI
	lookupPair func(context.Context, string) (map[string]string, error)
	syncIndex  func(context.Context, gocql.UUID, string) error
	events     activationEvents
	peers      brigPeers
}

type activateRequest struct {
	key, code string
	dryrun    bool
}

func decodeActivate(reader io.Reader) (activateRequest, error) {
	invalid := errors.New("invalid activation request")
	var body map[string]json.RawMessage
	decoder := json.NewDecoder(reader)
	var trailing any
	if decoder.Decode(&body) != nil || body == nil || decoder.Decode(&trailing) != io.EOF {
		return activateRequest{}, invalid
	}
	stringValue := func(name string, required bool) (*string, error) {
		raw, exists := body[name]
		if !exists || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			if required {
				return nil, invalid
			}
			return nil, nil
		}
		var value string
		if !utf8.Valid(raw) || json.Unmarshal(raw, &value) != nil || !utf8.ValidString(value) {
			return nil, invalid
		}
		return &value, nil
	}
	key, err := stringValue("key", false)
	if err != nil {
		return activateRequest{}, err
	}
	email, err := stringValue("email", false)
	if err != nil {
		return activateRequest{}, err
	}
	if key != nil && !validActivationValue(*key) {
		return activateRequest{}, invalid
	}
	if email != nil {
		parsed, err := mail.ParseAddress(*email)
		if err != nil || parsed.Address != *email {
			return activateRequest{}, invalid
		}
	}
	code, err := stringValue("code", true)
	if err != nil || !validActivationValue(*code) {
		return activateRequest{}, invalid
	}
	// Native Schema Activate declares dryrun as a required field.
	raw, present := body["dryrun"]
	var dryrun bool
	if !present || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &dryrun) != nil {
		return activateRequest{}, invalid
	}
	request := activateRequest{code: *code, dryrun: dryrun}
	switch {
	case key != nil:
		request.key = *key
	case email != nil:
		request.key, err = activationEmailKey(*email)
	default:
		return activateRequest{}, invalid
	}
	return request, err
}
func activationHTTPError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errInvalidActivationCode):
		authenticationWireError(w, 404, "invalid-code", "Invalid activation code")
	case errors.Is(err, errActivationWrongUser):
		authenticationWireError(w, 404, "invalid-code", "User does not exist")
	case errors.Is(err, errActivationKeyExists):
		authenticationWireError(w, 409, "key-exists", "The given e-mail address is in use.")
	case errors.Is(err, errDomainRegistrationSSO):
		authenticationWireError(w, 403, "condition-failed", "`domain_redirect` is set to `sso:{code}`")
	case errors.Is(err, errDomainRegistrationBackend):
		authenticationWireError(w, 403, "condition-failed", "`domain_redirect` is set to `backend`")
	case errors.Is(err, errDomainRegistrationDisabled):
		authenticationWireError(w, 403, "condition-failed", "`domain_redirect` is set to `no-registration`")
	case errors.Is(err, errInvalidEmailDomain):
		authenticationWireError(w, 400, "invalid-domain", "Invalid domain")
	default:
		authenticationWireError(w, 500, "server-error", "Internal Server Error")
	}
}

func (h activationHTTP) forRequest(requestID string, currency *string) activationAPI {
	service := h.service
	service.syncIndex = func(ctx context.Context, user gocql.UUID) error {
		if h.syncIndex == nil {
			return errors.New("activation index unavailable")
		}
		return h.syncIndex(ctx, user, requestID)
	}
	service.generateEvent = func(ctx context.Context, result *activatedIdentity) error {
		return h.events.generate(ctx, result, requestID)
	}
	service.activateTeam = func(ctx context.Context, user gocql.UUID) error {
		return h.peers.activateBindingTeamCurrency(ctx, user, requestID, currency)
	}
	return service
}
func (h activationHTTP) handle(w http.ResponseWriter, r *http.Request) {
	var request activateRequest
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		for _, name := range []string{"key", "code"} {
			values, present := r.URL.Query()[name]
			if !present || len(values) == 0 {
				routeParseError(w, "Query parameter "+name+" is required")
				return
			}
			if !validActivationValue(values[0]) {
				routeParseError(w, "Error parsing query parameter "+name+" failed: invalid activation value")
				return
			}
			if name == "key" {
				request.key = values[0]
			} else {
				request.code = values[0]
			}
		}
	case http.MethodPost:
		var err error
		request, err = decodeActivate(r.Body)
		if err != nil {
			authenticationWireError(w, 400, "bad-request", "Invalid activation request")
			return
		}
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	requestID := r.Header.Get("Request-Id")
	service := h.forRequest(requestID, nil)
	result, err := service.run(r.Context(), request.key, request.code, request.dryrun, true, nil)
	if err != nil {
		activationHTTPError(w, err)
		return
	}
	if request.dryrun {
		w.WriteHeader(http.StatusOK)
		return
	}
	if result == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	readJSON(w, r, map[string]any{"email": result.email, "first": result.first})
}

func (h activationHTTP) handleInternalPair(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	values, present := r.URL.Query()["email"]
	if !present || len(values) == 0 {
		routeParseError(w, "Query parameter email is required")
		return
	}
	email, err := canonicalRegistrationEmail(values[0])
	if err != nil {
		routeParseError(w, "Error parsing query parameter email failed: Invalid email")
		return
	}
	if h.lookupPair == nil {
		authenticationWireError(w, 500, "server-error", "Internal Server Error")
		return
	}
	pair, err := h.lookupPair(r.Context(), email)
	if err != nil {
		authenticationWireError(w, 500, "server-error", "Internal Server Error")
		return
	}
	if pair == nil {
		authenticationWireError(w, 404, "not-found", "Activation key not found.")
		return
	}
	readJSON(w, r, pair)
}
