// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

// These methods are a persistent service boundary, not an in-memory substitute.
type settingsStore interface {
	defaultSSO(context.Context) (*gocql.UUID, error)
	setDefaultSSO(context.Context, *gocql.UUID) error
	userCreated(context.Context, gocql.UUID) (*time.Time, error)
}

var errIdPNotFound = errors.New("IdP not found")

func jsonResponse(w http.ResponseWriter, r *http.Request, value any) {
	w.Header().Set("Content-Type", "application/json;charset=utf-8")
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	_ = json.NewEncoder(w).Encode(value)
}

func serveSettings(w http.ResponseWriter, r *http.Request, store settingsStore) bool {
	if r.URL.Path == "/sso/settings" {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return true
		}
		value, err := store.defaultSSO(r.Context())
		if err != nil {
			wireError(w, 500, "server-error", "Server Error")
			return true
		}
		jsonResponse(w, r, map[string]any{"default_sso_code": value})
		return true
	}
	if r.URL.Path == "/i/sso/settings" {
		if r.Method != http.MethodPut {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return true
		}
		var object map[string]json.RawMessage
		decoder := json.NewDecoder(r.Body)
		err := decoder.Decode(&object)
		var extra any
		if err != nil || decoder.Decode(&extra) != io.EOF || object == nil {
			wireError(w, 400, "bad-request", "Invalid SSO settings")
			return true
		}
		raw, present := object["default_sso_code"]
		if !present {
			wireError(w, 400, "bad-request", "Invalid SSO settings")
			return true
		}
		var id *gocql.UUID
		if string(raw) != "null" {
			var encoded string
			if json.Unmarshal(raw, &encoded) != nil {
				wireError(w, 400, "bad-request", "Invalid SSO settings")
				return true
			}
			parsed, parseErr := gocql.ParseUUID(encoded)
			if parseErr != nil {
				wireError(w, 400, "bad-request", "Invalid SSO settings")
				return true
			}
			id = &parsed
		}
		if err = store.setDefaultSSO(r.Context(), id); err != nil {
			if errors.Is(err, errIdPNotFound) {
				wireError(w, 404, "not-found", "Could not find IdP.")
			} else {
				wireError(w, 500, "server-error", "Server Error")
			}
			return true
		}
		w.Header().Set("Content-Type", "application/json;charset=utf-8")
		w.WriteHeader(http.StatusOK)
		return true
	}
	const prefix = "/i/scim/userinfo/"
	if !strings.HasPrefix(r.URL.Path, prefix) || strings.Contains(strings.TrimPrefix(r.URL.Path, prefix), "/") {
		return false
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return true
	}
	id, err := gocql.ParseUUID(strings.TrimPrefix(r.URL.Path, prefix))
	if err != nil {
		wireError(w, 400, "bad-request", "Invalid user ID")
		return true
	}
	created, err := store.userCreated(r.Context(), id)
	if err != nil {
		wireError(w, 500, "server-error", "Server Error")
		return true
	}
	value := map[string]any{"id": id.String()}
	if created != nil {
		value["created_on"] = created.UTC().Format("2006-01-02T15:04:05.000Z")
	}
	jsonResponse(w, r, value)
	return true
}
