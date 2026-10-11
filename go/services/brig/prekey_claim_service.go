// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/apache/cassandra-gocql-driver/v2"
)

var errPrekeyConsent = errors.New("missing legalhold consent")
var errPrekeyOldConsent = errors.New("missing legalhold consent old clients")

type prekeyClaimService struct {
	domain          string
	maxUsers        int
	remote          *prekeyFederationClient
	claim           func(context.Context, gocql.UUID, string) (*uncheckedPrekey, error)
	clients         func(context.Context, gocql.UUID) ([]storedClient, error)
	lookup          func(context.Context, gocql.UUID, string) (*storedClient, error)
	remove          func(context.Context, gocql.UUID, *string, storedClient) error
	federationGuard func(context.Context, map[string][]string, string) error
	guard           func(context.Context, gocql.UUID, map[string][]string, string) error
}

func (s prekeyClaimService) single(ctx context.Context, protectee, user gocql.UUID, id, requestID string) (*uncheckedPrekey, error) {
	if err := s.guard(ctx, protectee, map[string][]string{user.String(): {id}}, requestID); err != nil {
		return nil, err
	}
	return s.claimOrClean(ctx, user, id)
}

func (s prekeyClaimService) claimOrClean(ctx context.Context, user gocql.UUID, id string) (*uncheckedPrekey, error) {
	key, err := s.claim(ctx, user, id)
	if err != nil || key != nil {
		return key, err
	}
	client, err := s.lookup(ctx, user, id)
	if err != nil || client == nil {
		return nil, err
	}
	if err := s.remove(ctx, user, nil, *client); err != nil {
		return nil, err
	}
	return nil, nil
}

func (s prekeyClaimService) bundle(ctx context.Context, protectee, user gocql.UUID, requestID string) ([]any, error) {
	clients, err := s.clients(ctx, user)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(clients))
	for _, client := range clients {
		ids = append(ids, client.ID)
	}
	if err := s.guard(ctx, protectee, map[string][]string{user.String(): ids}, requestID); err != nil {
		return nil, err
	}
	result := []any{}
	for _, client := range clients {
		key, err := s.claim(ctx, user, client.ID)
		if err != nil {
			return nil, err
		}
		// Native bundle omits missing keys and does not run noPrekeys cleanup.
		if key != nil {
			result = append(result, map[string]any{"client": client.ID, "prekey": key})
		}
	}
	return result, nil
}

func prekeyClaimHTTPError(w http.ResponseWriter, err error) {
	var federation prekeyFederationFailure
	if errors.As(err, &federation) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(federation.code)
		_ = json.NewEncoder(w).Encode(federation.wire())
		return
	}

	switch {
	case errors.Is(err, errPrekeyConsent):
		authenticationWireError(w, 403, "missing-legalhold-consent", "Failed to connect to a user or to invite a user to a group because somebody is under legalhold and somebody else has not granted consent")
	case errors.Is(err, errPrekeyOldConsent):
		authenticationWireError(w, 403, "missing-legalhold-consent-old-clients", "Failed to connect to a user or to invite a user to a group because somebody is under legalhold and somebody else has old clients that do not support legalhold's UI requirements")
	default:
		authenticationHTTPError(w, err)
	}
}

func (s prekeyClaimService) handle(w http.ResponseWriter, r *http.Request, path string) bool {
	if s.handleFederation(w, r, path) || s.handleMulti(w, r, path) {
		return true
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 3 || parts[0] != "users" {
		return false
	}
	qualified := len(parts) >= 4 && parts[3] == "prekeys"
	unqualified := parts[2] == "prekeys"
	if !qualified && !unqualified {
		return false
	}
	index := 3
	domain := s.domain
	if qualified {
		index = 4
		var err error
		domain, err = parseEmailDomain(parts[1])
		if err != nil {
			routeParseError(w, "invalid domain")
			return true
		}
	}
	if len(parts) != index && len(parts) != index+1 {
		return false
	}
	if unqualified {
		version, _ := strconv.Atoi(r.Header.Get("X-Wire-API-Version"))
		if version >= 2 {
			return false
		}
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(405)
		return true
	}
	protectee, ok := userHeader(w, r)
	if !ok {
		return true
	}
	uidIndex := 1
	if qualified {
		uidIndex = 2
	}
	canonical, valid := tokenUUID(parts[uidIndex])
	if !valid {
		routeParseError(w, "invalid UUID")
		return true
	}
	user, _ := gocql.ParseUUID(canonical)
	if domain != s.domain {
		if s.remote == nil {
			prekeyClaimHTTPError(w, prekeyFederationFailure{code: 400, label: "federation-not-enabled", message: "no federator configured"})
			return true
		}
		if len(parts) == index {
			body, err := s.remote.bundle(r.Context(), domain, user, r.Header.Get("Request-Id"))
			if err != nil {
				prekeyClaimHTTPError(w, err)
			} else {
				readJSON(w, r, body)
			}
			return true
		}
		id, err := parseClientID(parts[index])
		if err != nil {
			routeParseError(w, "invalid client ID")
			return true
		}
		key, err := s.remote.single(r.Context(), domain, user, id, r.Header.Get("Request-Id"))
		if err != nil {
			prekeyClaimHTTPError(w, err)
		} else if key == nil {
			authenticationWireError(w, 404, "not-found", "prekey not found")
		} else {
			readJSON(w, r, key)
		}
		return true
	}
	if len(parts) == index {
		keys, err := s.bundle(r.Context(), protectee, user, r.Header.Get("Request-Id"))
		if err != nil {
			prekeyClaimHTTPError(w, err)
			return true
		}
		readJSON(w, r, map[string]any{"user": user.String(), "clients": keys})
		return true
	}
	id, err := parseClientID(parts[index])
	if err != nil {
		routeParseError(w, "invalid client ID")
		return true
	}
	key, err := s.single(r.Context(), protectee, user, id, r.Header.Get("Request-Id"))
	if err != nil {
		prekeyClaimHTTPError(w, err)
		return true
	}
	if key == nil {
		authenticationWireError(w, 404, "not-found", "prekey not found")
		return true
	}
	readJSON(w, r, map[string]any{"client": id, "prekey": key})
	return true
}
