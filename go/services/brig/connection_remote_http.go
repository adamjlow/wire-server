// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"encoding/json"
	"github.com/apache/cassandra-gocql-driver/v2"
	"net/http"
	"strings"
)

func (s connectionRemoteService) handle(w http.ResponseWriter, r *http.Request, path string) bool {
	if path == "/federation/send-connection-action" {
		return s.handleIncoming(w, r)
	}
	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		return false
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 3 || parts[0] != "connections" {
		return false
	}
	domain, err := parseEmailDomain(parts[1])
	if err != nil {
		routeParseError(w, "invalid domain")
		return true
	}
	if domain == s.local.domain {
		return false
	}
	self, ok := userHeader(w, r)
	if !ok {
		return true
	}
	connection, exists := r.Header["Z-Connection"]
	if !exists || len(connection) == 0 {
		routeParseError(w, "Header Z-Connection is required")
		return true
	}
	targetText, ok := tokenUUID(parts[2])
	if !ok {
		routeParseError(w, "invalid UUID")
		return true
	}
	target, _ := gocql.ParseUUID(targetText)
	var row *storedConnection
	created := false
	if r.Method == http.MethodPost {
		row, created, err = s.create(r.Context(), self, target, domain, &connection[0], r.Header.Get("Request-Id"))
	} else {
		if !publicClientContentType(w, r) {
			return true
		}
		raw, e := publicClientJSON(r)
		if e != nil {
			routeParseError(w, "Invalid connection update")
			return true
		}
		var body struct {
			Status *string `json:"status"`
		}
		if json.Unmarshal(raw, &body) != nil || body.Status == nil {
			routeParseError(w, "Invalid connection update")
			return true
		}
		relation := int32(-1)
		for i, name := range []string{"accepted", "blocked", "pending", "ignored", "sent", "cancelled", "missing-legalhold-consent"} {
			if *body.Status == name {
				relation = int32(i)
				break
			}
		}
		if relation < 0 {
			routeParseError(w, "Invalid connection update")
			return true
		}
		row, err = s.update(r.Context(), self, target, domain, relation, &connection[0], r.Header.Get("Request-Id"))
	}
	if err != nil {
		connectionHTTPError(w, err)
		return true
	}
	if row == nil {
		w.WriteHeader(204)
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
func (s connectionRemoteService) handleIncoming(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return true
	}
	domain, err := parseEmailDomain(r.Header.Get("Wire-Origin-Domain"))
	if err != nil {
		routeParseError(w, "Invalid federation origin domain")
		return true
	}
	if !publicClientContentType(w, r) {
		return true
	}
	raw, err := publicClientJSON(r)
	if err != nil {
		routeParseError(w, "Invalid federation connection action")
		return true
	}
	var body struct {
		From   *string `json:"from"`
		To     *string `json:"to"`
		Team   *string `json:"from_team"`
		Action *string `json:"action"`
	}
	if json.Unmarshal(raw, &body) != nil || body.From == nil || body.To == nil || body.Action == nil {
		routeParseError(w, "Invalid federation connection action")
		return true
	}
	from, ok := tokenUUID(*body.From)
	if !ok {
		routeParseError(w, "invalid UUID")
		return true
	}
	to, ok := tokenUUID(*body.To)
	if !ok {
		routeParseError(w, "invalid UUID")
		return true
	}
	target, _ := gocql.ParseUUID(from)
	self, _ := gocql.ParseUUID(to)
	var team *gocql.UUID
	if body.Team != nil {
		canonical, ok := tokenUUID(*body.Team)
		if !ok {
			routeParseError(w, "invalid UUID")
			return true
		}
		id, _ := gocql.ParseUUID(canonical)
		team = &id
	}
	action := ""
	switch *body.Action {
	case "RemoteConnect":
		action = "connect"
	case "RemoteRescind":
		action = "rescind"
	default:
		routeParseError(w, "Invalid federation connection action")
		return true
	}
	result, err := s.incoming(r.Context(), self, target, domain, team, action, r.Header.Get("Request-Id"))
	if err != nil {
		connectionHTTPError(w, err)
	} else {
		readJSON(w, r, result)
	}
	return true
}
