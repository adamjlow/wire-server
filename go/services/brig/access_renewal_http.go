// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type accessRenewalHTTP struct {
	service        accessRenewalService
	syncIndex      func(context.Context, gocql.UUID, string) error
	cookieInsecure bool
}

func inactiveTimeout(c Config) (*time.Duration, error) {
	if c.Settings.SuspendInactive == nil {
		return nil, nil
	}
	seconds := c.Settings.SuspendInactive.Timeout
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds >= float64(math.MaxInt64)/1e9 || seconds <= float64(math.MinInt64)/1e9 {
		return nil, errors.New("invalid inactive account timeout")
	}
	timeout := time.Duration(seconds * 1e9)
	return &timeout, nil
}

func (s accessRenewalHTTP) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var client *string
	if values, exists := r.URL.Query()["client_id"]; exists && len(values) != 0 {
		id, err := parseClientID(values[0])
		if err != nil {
			routeParseError(w, "Error parsing query parameter client_id failed: invalid client ID")
			return
		}
		client = &id
	}
	credentials, err := parseAccessCredentials(r)
	if err != nil {
		tokenHTTPError(w, err)
		return
	}
	service := s.service
	if s.syncIndex != nil {
		service.accounts.syncIndex = func(ctx context.Context, user gocql.UUID) error {
			return s.syncIndex(ctx, user, r.Header.Get("Request-Id"))
		}
	}
	renewed, err := service.renew(r.Context(), credentials, client)
	if err != nil {
		tokenHTTPError(w, err)
		return
	}
	encoded, err := renewed.token.Encode()
	if err != nil {
		tokenHTTPError(w, err)
		return
	}
	if renewed.cookie != nil {
		value, err := renewed.cookie.token.Encode()
		if err != nil {
			tokenHTTPError(w, err)
			return
		}
		cookie := http.Cookie{Name: "zuid", Value: value, Path: "/access", HttpOnly: true, Secure: !s.cookieInsecure}
		if renewed.cookie.record.Type == 1 {
			cookie.Expires = renewed.cookie.record.Expires
		}
		http.SetCookie(w, &cookie)
	}
	w.Header().Set("Content-Type", "application/json;charset=utf-8")
	_ = json.NewEncoder(w).Encode(struct {
		User    string `json:"user"`
		Access  string `json:"access_token"`
		Type    string `json:"token_type"`
		Expires int64  `json:"expires_in"`
	}{renewed.token.User, encoded, "Bearer", renewed.expiresIn})
}
