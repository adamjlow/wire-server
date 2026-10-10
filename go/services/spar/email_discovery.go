// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type emailDiscoveryStore interface {
	idpsByTeam(context.Context, gocql.UUID) ([]idpConfig, error)
}

func discoverEmail(ctx context.Context, c Config, s emailDiscoveryStore, p peerClient, email, host, requestID string) (*gocql.UUID, error) {
	if c.EnableEmailDiscovery == nil || !*c.EnableEmailDiscovery {
		return nil, nil
	}
	status, body, err := p.request(ctx, c.Brig, "GET", "/i/users?"+url.Values{"email": {email}}.Encode(), requestID, "", nil)
	if err != nil {
		return nil, err
	}
	var users []brigAccount
	if status < 200 || status >= 300 || json.Unmarshal(body, &users) != nil {
		return nil, errors.New("invalid Brig email discovery response")
	}
	if len(users) > 1 {
		return nil, &apiError{500, "server-error", "Internal error: Multiple users found for email address"}
	}
	if len(users) == 0 || users[0].SSOID == nil {
		return nil, nil
	}
	status, body, err = p.request(ctx, c.Galley, "GET", "/i/users/"+users[0].ID.String()+"/team", requestID, "", nil)
	if err != nil || status == 404 {
		return nil, err
	}
	var team gocql.UUID
	if status != 200 || json.Unmarshal(body, &team) != nil {
		return nil, errors.New("invalid Galley email discovery team response")
	}
	idps, err := s.idpsByTeam(ctx, team)
	if err != nil {
		return nil, err
	}
	for _, idp := range idps {
		if host == "" && idp.Extra.Domain == nil || idp.Extra.Domain != nil && *idp.Extra.Domain == host {
			return &idp.ID, nil
		}
	}
	return nil, nil
}

func serveEmailDiscovery(w http.ResponseWriter, r *http.Request, c Config, s emailDiscoveryStore, p peerClient) bool {
	if r.URL.Path != "/sso/get-by-email" {
		return false
	}
	version, _ := strconv.Atoi(r.Header.Get("X-Wire-API-Version"))
	if version < 15 {
		w.WriteHeader(http.StatusNotFound)
		return true
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return true
	}
	object, err := decodeObject(r)
	var email string
	if err != nil || json.Unmarshal(object["email"], &email) != nil || !scimEmailAddress(email) {
		domainError(w, &apiError{400, "bad-request", "Invalid email address"})
		return true
	}
	id, err := discoverEmail(r.Context(), c, s, p, email, r.Header.Get("Z-Host"), r.Header.Get("Request-Id"))
	if err != nil {
		domainError(w, err)
		return true
	}
	w.Header().Set("Content-Type", "application/json;charset=utf-8")
	body := map[string]any{}
	if id == nil {
		w.WriteHeader(http.StatusNotFound)
	} else {
		body["sso_code"] = id
	}
	_ = json.NewEncoder(w).Encode(body)
	return true
}
