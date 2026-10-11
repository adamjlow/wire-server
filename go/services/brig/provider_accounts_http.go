// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type providerAccountHTTP struct {
	lookup              func(context.Context, gocql.UUID) (*providerAccount, error)
	update              func(context.Context, gocql.UUID, providerProfileUpdate) error
	secondFactorEnabled func(context.Context, string) (bool, error)
}

func providerHeader(w http.ResponseWriter, r *http.Request) (gocql.UUID, bool) {
	values := r.Header.Values("Z-Provider")
	if len(values) == 0 {
		routeParseError(w, "Header Z-Provider is required")
		return gocql.UUID{}, false
	}
	canonical, valid := tokenUUID(values[0])
	if !valid {
		routeParseError(w, "Error parsing header Z-Provider failed: invalid UUID")
		return gocql.UUID{}, false
	}
	id, _ := gocql.ParseUUID(canonical)
	return id, true
}

func (h providerAccountHTTP) handle(w http.ResponseWriter, r *http.Request, path string) bool {
	public := strings.HasPrefix(path, "/providers/") && len(strings.Split(strings.Trim(path, "/"), "/")) == 2
	if path != "/provider" && !public {
		return false
	}
	allowed := r.Method == http.MethodGet || r.Method == http.MethodHead || !public && r.Method == http.MethodPut
	if !allowed {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return true
	}
	var id gocql.UUID
	var valid bool
	if public {
		if _, valid = userHeader(w, r); !valid {
			return true
		}
		canonical, ok := tokenUUID(strings.TrimPrefix(path, "/providers/"))
		if !ok {
			routeParseError(w, "invalid provider ID")
			return true
		}
		id, _ = gocql.ParseUUID(canonical)
	} else if id, valid = providerHeader(w, r); !valid {
		return true
	}
	var update providerProfileUpdate
	if r.Method == http.MethodPut {
		if !publicClientContentType(w, r) {
			return true
		}
		raw, err := publicClientJSON(r)
		if err == nil {
			update, err = decodeProviderProfileUpdate(raw)
		}
		if err != nil {
			authenticationWireError(w, 400, "bad-request", "Invalid provider update")
			return true
		}
	}
	if h.secondFactorEnabled == nil || h.lookup == nil {
		registrationHTTPError(w, errors.New("provider dependencies unavailable"))
		return true
	}
	enabled, err := h.secondFactorEnabled(r.Context(), r.Header.Get("Request-Id"))
	if err != nil {
		registrationHTTPError(w, err)
		return true
	}
	if enabled {
		authenticationWireError(w, 403, "access-denied", "Access denied.")
		return true
	}
	account, err := h.lookup(r.Context(), id)
	if err != nil {
		registrationHTTPError(w, err)
	} else if account == nil {
		authenticationWireError(w, 404, "not-found", "Provider not found.")
	} else if r.Method == http.MethodPut {
		if h.update == nil {
			registrationHTTPError(w, errors.New("provider update unavailable"))
		} else if err := h.update(r.Context(), id, update); err != nil {
			registrationHTTPError(w, err)
		} else {
			w.WriteHeader(http.StatusOK)
		}
	} else {
		readJSON(w, r, account)
	}
	return true
}
