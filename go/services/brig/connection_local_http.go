// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"encoding/json"
	"errors"
	"github.com/apache/cassandra-gocql-driver/v2"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"
)

func connectionHTTPError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errConnectionNotFederating):
		authenticationWireError(w, 403, "team-not-federating", "The target user is owned by a federated backend, but is not in an allow-listed team")
	case errors.Is(err, errConnectionInvalidUser):
		authenticationWireError(w, 400, "invalid-user", "Invalid user")
	case errors.Is(err, errConnectionNoIdentity):
		authenticationWireError(w, 403, "no-identity", "The user has no verified email")
	case errors.Is(err, errConnectionSameTeam):
		authenticationWireError(w, 403, "same-binding-team-users", "Operation not allowed to binding team users.")
	case errors.Is(err, errConnectionTransition):
		authenticationWireError(w, 403, "bad-conn-update", "Invalid status transition")
	case errors.Is(err, errConnectionLimit):
		authenticationWireError(w, 403, "connection-limit", "Too many sent/accepted connections")
	case errors.Is(err, errPrekeyConsent):
		prekeyClaimHTTPError(w, err)
	default:
		prekeyClaimHTTPError(w, err)
	}
}
func (s connectionLocalService) handle(w http.ResponseWriter, r *http.Request, path string) bool {
	if r.Method != http.MethodPost {
		return false
	}
	version, _ := strconv.Atoi(r.Header.Get("X-Wire-API-Version"))
	parts := strings.Split(strings.Trim(path, "/"), "/")
	legacy := path == "/connections" && version < 2
	if !legacy && (len(parts) != 3 || parts[0] != "connections") {
		return false
	}
	if !legacy {
		domain, err := parseEmailDomain(parts[1])
		if err != nil {
			routeParseError(w, "invalid domain")
			return true
		}
		if domain != s.domain {
			return false
		}
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
	var targetText string
	if legacy {
		if !publicClientContentType(w, r) {
			return true
		}
		raw, err := publicClientJSON(r)
		if err != nil {
			routeParseError(w, "Invalid connection request")
			return true
		}
		var body struct {
			User *string `json:"user"`
			Name *string `json:"name"`
		}
		if json.Unmarshal(raw, &body) != nil || body.User == nil || body.Name == nil || utf8.RuneCountInString(*body.Name) < 1 || utf8.RuneCountInString(*body.Name) > 256 {
			routeParseError(w, "Invalid connection request")
			return true
		}
		targetText = *body.User
	} else {
		targetText = parts[2]
	}
	canonical, ok := tokenUUID(targetText)
	if !ok {
		routeParseError(w, "invalid UUID")
		return true
	}
	target, _ := gocql.ParseUUID(canonical)
	row, created, err := s.create(r.Context(), user, target, connection[0], r.Header.Get("Request-Id"))
	if err != nil {
		connectionHTTPError(w, err)
		return true
	}
	wire, err := row.wire()
	if err != nil {
		connectionHTTPError(w, err)
		return true
	}
	w.Header().Set("Content-Type", "application/json;charset=utf-8")
	if created {
		w.WriteHeader(201)
	}
	_ = json.NewEncoder(w).Encode(wire)
	return true
}
