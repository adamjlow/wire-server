// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func publicClientJSON(r *http.Request) (json.RawMessage, error) {
	decoder := json.NewDecoder(r.Body)
	var raw json.RawMessage
	var extra any
	if decoder.Decode(&raw) != nil || decoder.Decode(&extra) != io.EOF {
		return nil, errors.New("invalid client request")
	}
	return federationJSON(raw)
}

func decodeQualifiedClientUsers(raw json.RawMessage) (map[string][]gocql.UUID, error) {
	var values []json.RawMessage
	if string(raw) == "null" || json.Unmarshal(raw, &values) != nil || len(values) < 1 || len(values) > 500 {
		return nil, errors.New("invalid qualified client users")
	}
	result := map[string][]gocql.UUID{}
	for _, value := range values {
		fields, err := registrationObject(value)
		if err != nil {
			return nil, err
		}
		id, err := registrationText(fields, "id", true)
		if err != nil {
			return nil, err
		}
		canonical, ok := tokenUUID(*id)
		if !ok {
			return nil, errors.New("invalid qualified client user")
		}
		domainText, err := registrationText(fields, "domain", true)
		if err != nil {
			return nil, err
		}
		domain, err := parseEmailDomain(*domainText)
		if err != nil {
			return nil, err
		}
		user, _ := gocql.ParseUUID(canonical)
		result[domain] = append(result[domain], user)
	}
	return result, nil
}

func publicClientContentType(w http.ResponseWriter, r *http.Request) bool {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(media, "application/json") {
		w.WriteHeader(415)
		return false
	}
	return true
}

func (s publicClientService) handle(w http.ResponseWriter, r *http.Request, path string) bool {
	version, _ := strconv.Atoi(r.Header.Get("X-Wire-API-Version"))
	if path == "/federation/get-user-clients" {
		if r.Method != http.MethodPost {
			w.WriteHeader(405)
			return true
		}
		origin := r.Header.Values("Wire-Origin-Domain")
		if len(origin) == 0 {
			routeParseError(w, "Header Wire-Origin-Domain is required")
			return true
		}
		if _, err := parseEmailDomain(origin[0]); err != nil {
			routeParseError(w, "Error parsing header Wire-Origin-Domain failed: invalid domain")
			return true
		}
		if !publicClientContentType(w, r) {
			return true
		}
		raw, err := publicClientJSON(r)
		var fields map[string]json.RawMessage
		if err == nil {
			fields, err = registrationObject(raw)
		}
		var ids []string
		if err != nil || !registrationPresent(fields["users"]) || json.Unmarshal(fields["users"], &ids) != nil {
			routeParseError(w, "Invalid federation client users")
			return true
		}
		users := make([]gocql.UUID, len(ids))
		for i, id := range ids {
			canonical, ok := tokenUUID(id)
			if !ok {
				routeParseError(w, "Invalid federation client users")
				return true
			}
			users[i], _ = gocql.ParseUUID(canonical)
		}
		result, err := s.local(r.Context(), users)
		if err != nil {
			authenticationHTTPError(w, err)
		} else {
			readJSON(w, r, result)
		}
		return true
	}
	if path == "/users/list-clients" || path == "/users/list-clients/v2" {
		legacyV2 := path == "/users/list-clients/v2"
		if legacyV2 && version >= 2 {
			return false
		}
		if r.Method != http.MethodPost {
			w.WriteHeader(405)
			return true
		}
		if _, ok := userHeader(w, r); !ok {
			return true
		}
		if !publicClientContentType(w, r) {
			return true
		}
		raw, err := publicClientJSON(r)
		wrapped := version >= 2 || legacyV2
		if err == nil && wrapped {
			var fields map[string]json.RawMessage
			fields, err = registrationObject(raw)
			raw = fields["qualified_users"]
		}
		var users map[string][]gocql.UUID
		if err == nil {
			users, err = decodeQualifiedClientUsers(raw)
		}
		if err != nil {
			routeParseError(w, "Invalid qualified client users")
			return true
		}
		result, err := s.qualified(r.Context(), users, r.Header.Get("Request-Id"))
		if err != nil {
			authenticationHTTPError(w, err)
		} else if wrapped {
			readJSON(w, r, map[string]any{"qualified_user_map": result})
		} else {
			readJSON(w, r, result)
		}
		return true
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 3 || parts[0] != "users" {
		return false
	}
	qualified := len(parts) >= 4 && parts[3] == "clients"
	index := 3
	uidIndex := 1
	domain := s.domain
	if qualified {
		index = 4
		uidIndex = 2
	} else if parts[2] != "clients" || version >= 2 {
		return false
	}
	if len(parts) != index && len(parts) != index+1 {
		return false
	}
	if qualified && len(parts) == index && version >= 16 {
		return false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(405)
		return true
	}
	if qualified {
		var err error
		domain, err = parseEmailDomain(parts[1])
		if err != nil {
			routeParseError(w, "invalid domain")
			return true
		}
	}
	canonical, ok := tokenUUID(parts[uidIndex])
	if !ok {
		routeParseError(w, "invalid UUID")
		return true
	}
	user, _ := gocql.ParseUUID(canonical)
	var clientID string
	if len(parts) == index+1 {
		var err error
		clientID, err = parseClientID(parts[index])
		if err != nil {
			routeParseError(w, "invalid client ID")
			return true
		}
	}
	result, err := s.qualified(r.Context(), map[string][]gocql.UUID{domain: {user}}, r.Header.Get("Request-Id"))
	if err != nil {
		authenticationHTTPError(w, err)
		return true
	}
	clients := result[domain][canonical]
	if clients == nil {
		clients = []publicClient{}
	}
	if len(parts) == index {
		readJSON(w, r, clients)
		return true
	}
	for _, client := range clients {
		if client.ID == clientID {
			readJSON(w, r, client)
			return true
		}
	}
	authenticationWireError(w, 404, "not-found", "client not found")
	return true
}
