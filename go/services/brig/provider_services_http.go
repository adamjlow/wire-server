// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/apache/cassandra-gocql-driver/v2"
)

var errProviderServiceNotFound = errors.New("service not found")

type providerServiceHTTP struct {
	create  *providerServiceCreation
	update  *providerServiceProfileUpdate
	auth    providerAuthHTTP
	get     func(context.Context, gocql.UUID, gocql.UUID, bool) (any, error)
	list    func(context.Context, gocql.UUID, bool) ([]any, error)
	lookup  func(context.Context, gocql.UUID, gocql.UUID) (*deletionServiceRecord, error)
	disable func(context.Context, gocql.UUID, gocql.UUID) error
	enqueue func(context.Context, gocql.UUID, gocql.UUID) error
}

func (s providerServiceHTTP) delete(ctx context.Context, provider, id gocql.UUID, password string) error {
	valid, err := s.auth.check(ctx, provider, password)
	if err != nil {
		return err
	}
	if !valid {
		return authInvalidCredentials
	}
	if s.lookup == nil || s.disable == nil || s.enqueue == nil {
		return errors.New("service deletion unavailable")
	}
	record, err := s.lookup(ctx, provider, id)
	if err != nil {
		return err
	}
	if record == nil {
		return errProviderServiceNotFound
	}
	if err := s.disable(ctx, provider, id); err != nil {
		return err
	}
	return s.enqueue(ctx, provider, id)
}

func (s providerServiceHTTP) handle(w http.ResponseWriter, r *http.Request, path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	catalog := path == "/services/tags"
	public := len(parts) >= 3 && parts[0] == "providers" && parts[2] == "services" && len(parts) <= 4
	private := len(parts) >= 2 && parts[0] == "provider" && parts[1] == "services" && len(parts) <= 3
	if !catalog && !public && !private {
		return false
	}
	if private && len(parts) == 2 && r.Method == http.MethodPost {
		if s.create == nil {
			return false
		}
		s.create.handle(w, r)
		return true
	}
	deleting := private && len(parts) == 3 && r.Method == http.MethodDelete
	updating := private && len(parts) == 3 && r.Method == http.MethodPut
	if !deleting && !updating && r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	var provider, id gocql.UUID
	if public || catalog {
		if _, ok := userHeader(w, r); !ok {
			return true
		}
	} else {
		var ok bool
		if provider, ok = providerHeader(w, r); !ok {
			return true
		}
	}
	if public {
		canonical, ok := tokenUUID(parts[1])
		if !ok {
			routeParseError(w, "Invalid provider ID")
			return true
		}
		provider, _ = gocql.ParseUUID(canonical)
	}
	single := public && len(parts) == 4 || private && len(parts) == 3
	if single {
		canonical, ok := tokenUUID(parts[len(parts)-1])
		if !ok {
			routeParseError(w, "Invalid service ID")
			return true
		}
		id, _ = gocql.ParseUUID(canonical)
	}
	if updating {
		if s.update == nil {
			authenticationHTTPError(w, errors.New("service update unavailable"))
			return true
		}
		s.update.handle(w, r, provider, id)
		return true
	}
	var password *string
	if deleting {
		if !publicClientContentType(w, r) {
			return true
		}
		raw, err := publicClientJSON(r)
		if err == nil {
			var fields map[string]json.RawMessage
			fields, err = registrationObject(raw)
			if err == nil {
				password, err = registrationText(fields, "password", true)
			}
		}
		if err != nil || validatePlainPassword(*password, 6) != nil {
			authenticationWireError(w, 400, "bad-request", "Invalid service deletion request")
			return true
		}
	}
	if s.auth.secondFactorEnabled == nil {
		authenticationHTTPError(w, errors.New("service feature lookup unavailable"))
		return true
	}
	enabled, err := s.auth.secondFactorEnabled(r.Context(), r.Header.Get("Request-Id"))
	if err != nil {
		authenticationHTTPError(w, err)
		return true
	}
	if enabled {
		authenticationWireError(w, 403, "access-denied", "Access denied.")
		return true
	}
	if catalog {
		readJSON(w, r, providerServiceTags)
		return true
	}
	if deleting {
		err := s.delete(r.Context(), provider, id, *password)
		if errors.Is(err, errProviderServiceNotFound) {
			authenticationWireError(w, 404, "not-found", "Service not found.")
		} else if err != nil {
			authenticationHTTPError(w, err)
		} else {
			w.WriteHeader(202)
		}
		return true
	}
	if single {
		if s.get == nil {
			authenticationHTTPError(w, errors.New("service lookup unavailable"))
			return true
		}
		value, err := s.get(r.Context(), provider, id, public)
		if err != nil {
			authenticationHTTPError(w, err)
		} else if value == nil {
			authenticationWireError(w, 404, "not-found", "Service not found.")
		} else {
			readJSON(w, r, value)
		}
	} else {
		if s.list == nil {
			authenticationHTTPError(w, errors.New("service listing unavailable"))
			return true
		}
		value, err := s.list(r.Context(), provider, public)
		if err != nil {
			authenticationHTTPError(w, err)
		} else {
			readJSON(w, r, value)
		}
	}
	return true
}
