// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
)

func (s clientDeletionService) handle(w http.ResponseWriter, r *http.Request, path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if r.Method != http.MethodDelete || len(parts) != 2 || parts[0] != "clients" {
		return false
	}
	user, ok := userHeader(w, r)
	if !ok {
		return true
	}
	values, exists := r.Header["Z-Connection"]
	if !exists || len(values) == 0 {
		routeParseError(w, "Header Z-Connection is required")
		return true
	}
	id, err := parseClientID(parts[1])
	if err != nil {
		routeParseError(w, "invalid client ID")
		return true
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(media, "application/json") {
		w.WriteHeader(415)
		return true
	}
	var raw json.RawMessage
	var extra any
	decoder := json.NewDecoder(r.Body)
	if decoder.Decode(&raw) != nil || decoder.Decode(&extra) != io.EOF {
		routeParseError(w, "Invalid client removal request")
		return true
	}
	parsed, err := parsePropertyValue(raw)
	if err != nil {
		routeParseError(w, "Invalid client removal request")
		return true
	}
	canonical, err := json.Marshal(parsed)
	if err != nil {
		routeParseError(w, "Invalid client removal request")
		return true
	}
	fields, err := registrationObject(canonical)
	if err != nil {
		routeParseError(w, "Invalid client removal request")
		return true
	}
	password, err := registrationText(fields, "password", false)
	if err != nil || password != nil && validatePlainPassword(*password, 6) != nil {
		routeParseError(w, "Invalid client removal request")
		return true
	}
	err = s.removePublic(r.Context(), user, id, values[0], password)
	switch {
	case err == nil:
		w.WriteHeader(200)
	case errors.Is(err, errClientMissing):
		authenticationWireError(w, 404, "client-not-found", "Client not found")
	case errors.Is(err, errClientLegalHoldDeletion):
		authenticationWireError(w, 400, "client-error", "LegalHold clients cannot be deleted. LegalHold must be disabled on this user by an admin")
	default:
		authenticationHTTPError(w, err)
	}
	return true
}
