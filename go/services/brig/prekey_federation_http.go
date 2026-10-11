// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func (s prekeyClaimService) handleFederation(w http.ResponseWriter, r *http.Request, path string) bool {
	if path != "/federation/claim-prekey" && path != "/federation/claim-prekey-bundle" && path != "/federation/claim-multi-prekey-bundle" {
		return false
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return true
	}
	values := r.Header.Values("Wire-Origin-Domain")
	if len(values) == 0 {
		routeParseError(w, "Header Wire-Origin-Domain is required")
		return true
	}
	if _, err := parseEmailDomain(values[0]); err != nil {
		routeParseError(w, "Error parsing header Wire-Origin-Domain failed: invalid domain")
		return true
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(media, "application/json") {
		w.WriteHeader(415)
		return true
	}
	decoder := json.NewDecoder(r.Body)
	var raw json.RawMessage
	var trailing any
	if decoder.Decode(&raw) != nil || decoder.Decode(&trailing) != io.EOF {
		routeParseError(w, "Invalid federation prekey request")
		return true
	}
	canonical, err := federationJSON(raw)
	if err != nil {
		routeParseError(w, "Invalid federation prekey request")
		return true
	}
	// Native calls Galley with this distinct protectee, whose current policy
	// does not enforce cross-federation consent. Preserve the RPC and its tag.
	s.guard = func(ctx context.Context, _ gocql.UUID, users map[string][]string, rid string) error {
		return s.federationGuard(ctx, users, rid)
	}
	rid := r.Header.Get("Request-Id")
	var result any
	switch path {
	case "/federation/claim-multi-prekey-bundle":
		var users map[string][]string
		users, err = decodePrekeyUserClients(canonical)
		if err != nil {
			routeParseError(w, "Invalid federation prekey request")
			return true
		}
		// This internal RPC has no public max-conversation-size guard.
		result, err = s.multiLocal(r.Context(), gocql.UUID{}, users, rid)
	default:
		var userText, clientText string
		if path == "/federation/claim-prekey" {
			var pair []string
			if json.Unmarshal(canonical, &pair) != nil || len(pair) != 2 {
				routeParseError(w, "Invalid federation prekey request")
				return true
			}
			userText, clientText = pair[0], pair[1]
		} else if json.Unmarshal(canonical, &userText) != nil {
			routeParseError(w, "Invalid federation prekey request")
			return true
		}
		userText, valid := tokenUUID(userText)
		if !valid {
			routeParseError(w, "Invalid federation prekey request")
			return true
		}
		user, _ := gocql.ParseUUID(userText)
		if path == "/federation/claim-prekey-bundle" {
			var clients []any
			clients, err = s.bundle(r.Context(), gocql.UUID{}, user, rid)
			result = map[string]any{"user": userText, "clients": clients}
		} else {
			client, parseErr := parseClientID(clientText)
			if parseErr != nil {
				routeParseError(w, "Invalid federation prekey request")
				return true
			}
			var key *uncheckedPrekey
			key, err = s.single(r.Context(), gocql.UUID{}, user, client, rid)
			if key != nil {
				result = federationClientPrekey{Client: client, Prekey: *key}
			}
		}
	}
	if err != nil {
		prekeyClaimHTTPError(w, err)
	} else {
		readJSON(w, r, result)
	}
	return true
}
