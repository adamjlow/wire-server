// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"strconv"
	"strings"
)

func (s clientRegistrationService) handle(w http.ResponseWriter, r *http.Request, path string) bool {
	if r.Method != http.MethodPost || path != "/clients" {
		return false
	}
	user, ok := userHeader(w, r)
	if !ok {
		return true
	}
	connection, exists := r.Header["Z-Connection"]
	if !exists || len(connection) == 0 {
		routeParseError(w, "Header Z-Connection is required")
		return true
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(media, "application/json") {
		w.WriteHeader(415)
		return true
	}
	version, _ := strconv.Atoi(r.Header.Get("X-Wire-API-Version"))
	request, err := decodeClientRegistration(r.Body, version)
	if err != nil {
		routeParseError(w, "Invalid client registration request")
		return true
	}
	client, err := s.addPublic(r.Context(), user, connection[0], request, r.Header.Get("Request-Id"))
	if err != nil {
		switch {
		case errors.Is(err, errClientUserMissing):
			authenticationWireError(w, 400, "invalid-user", "Invalid user")
		case errors.Is(err, errClientLegalHoldRegistration):
			authenticationWireError(w, 400, "client-error", "LegalHold clients cannot be added manually. LegalHold must be enabled on this user by an admin")
		case errors.Is(err, errClientCodeRequired):
			authenticationWireError(w, 403, "code-authentication-required", "Verification code required.")
		case errors.Is(err, errClientCodeFailed):
			authenticationWireError(w, 403, "code-authentication-failed", "Code authentication failed.")
		case errors.Is(err, errClientCapacity):
			authenticationWireError(w, 403, "too-many-clients", "Too many clients")
		case errors.Is(err, errPrekeyBundle):
			authenticationWireError(w, 400, "bad-request", "Malformed prekeys uploaded")
		case errors.Is(err, errClientMLSDuplicate):
			authenticationWireError(w, 400, "mls-duplicate-public-key", "MLS public key for the given signature scheme already exists")
		default:
			authenticationHTTPError(w, err)
		}
		return true
	}
	view, err := client.wire(version)
	if err != nil {
		authenticationHTTPError(w, err)
		return true
	}
	w.Header().Set("Content-Type", "application/json;charset=utf-8")
	w.Header().Set("Location", client.ID)
	w.WriteHeader(201)
	_ = json.NewEncoder(w).Encode(view)
	return true
}
