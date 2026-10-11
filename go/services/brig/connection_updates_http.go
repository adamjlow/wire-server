// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"encoding/json"
	"github.com/apache/cassandra-gocql-driver/v2"
	"net/http"
	"strconv"
	"strings"
)

func (s connectionUpdateService) handle(w http.ResponseWriter, r *http.Request, path string) bool {
	if r.Method != http.MethodPut {
		return false
	}
	version, _ := strconv.Atoi(r.Header.Get("X-Wire-API-Version"))
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 2 && len(parts) != 3 || parts[0] != "connections" || len(parts) == 2 && version >= 2 {
		return false
	}
	targetText := parts[len(parts)-1]
	if len(parts) == 3 {
		domain, err := parseEmailDomain(parts[1])
		if err != nil {
			routeParseError(w, "invalid domain")
			return true
		}
		if domain != s.local.domain {
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
	canonical, ok := tokenUUID(targetText)
	if !ok {
		routeParseError(w, "invalid UUID")
		return true
	}
	target, _ := gocql.ParseUUID(canonical)
	if !publicClientContentType(w, r) {
		return true
	}
	raw, err := publicClientJSON(r)
	if err != nil {
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
	row, err := s.apply(r.Context(), user, target, relation, &connection[0], r.Header.Get("Request-Id"))
	if err != nil {
		if err == errConnectionNotConnected {
			authenticationWireError(w, 403, "not-connected", "Users are not connected")
		} else {
			connectionHTTPError(w, err)
		}
		return true
	}
	if row == nil {
		w.WriteHeader(204)
		return true
	}
	wire, err := row.wire()
	if err != nil {
		connectionHTTPError(w, err)
	} else {
		readJSON(w, r, wire)
	}
	return true
}
