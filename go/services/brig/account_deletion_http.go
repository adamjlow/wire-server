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

type accountDeletionHTTP struct {
	service      accountDeletionService
	verification accountDeletionVerification
	keys         func(context.Context, gocql.UUID) ([]string, error)
	count        func(context.Context, gocql.UUID) (int64, error)
	cookies      func(context.Context, gocql.UUID) ([]sessionCookie, error)
}

func (h accountDeletionHTTP) ensure(ctx context.Context, id gocql.UUID, requestID string) (bool, error) {
	if h.service.get == nil || h.service.clients == nil || h.keys == nil || h.count == nil || h.cookies == nil {
		return false, errors.New("account deletion checks unavailable")
	}
	user, err := h.service.get(ctx, id)
	if err != nil {
		return false, err
	}
	if user == nil {
		return false, errAccountDeletionNotFound
	}
	keys, err := h.keys(ctx, id)
	if err != nil {
		return false, err
	}
	clients, err := h.service.clients(ctx, id)
	if err != nil {
		return false, err
	}
	count, err := h.count(ctx, id)
	if err != nil {
		return false, err
	}
	cookies, err := h.cookies(ctx, id)
	if err != nil {
		return false, err
	}
	already := user.Status != nil && *user.Status == 2 && len(keys) == 0 && len(clients) == 0 && count == 0 && len(cookies) == 0
	if already {
		return true, nil
	}
	return false, h.service.execute(ctx, *user, requestID)
}

var errAccountDeletionNotFound = errors.New("deletion account not found")

func accountDeletionHTTPError(w http.ResponseWriter, err error) {
	var pending deletionCodePending
	var throttle deletionCodeThrottled
	switch {
	case errors.Is(err, errAccountDeletionNotFound):
		authenticationWireError(w, 404, "not-found", "User not found")
	case errors.Is(err, errDeleteUserInvalid):
		authenticationWireError(w, 400, "invalid-user", "Invalid user")
	case errors.Is(err, errDeleteUserInvalidCode):
		authenticationWireError(w, 403, "invalid-code", "Invalid verification code")
	case errors.Is(err, errDeleteUserOwner):
		authenticationWireError(w, 403, "no-self-delete-for-team-owner", "Team owners are not allowed to delete themselves; ask a fellow owner")
	case errors.As(err, &pending):
		w.Header().Set("Content-Type", "application/json;charset=utf-8")
		w.WriteHeader(403)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 403, "label": "pending-delete", "message": "A verification code for account deletion is still pending.", "expires_in": pending.ttl})
	case errors.As(err, &throttle):
		w.Header().Set("Retry-After", strconv.FormatInt(int64(throttle.ttl), 10))
		authenticationWireError(w, 429, "too-many-requests", "Too many request to generate a verification code.")
	default:
		authenticationHTTPError(w, err)
	}
}

func (h accountDeletionHTTP) handle(w http.ResponseWriter, r *http.Request, path string) bool {
	self := path == "/self" && r.Method == http.MethodDelete
	verify := path == "/delete"
	parts := strings.Split(strings.Trim(path, "/"), "/")
	internal := len(parts) == 3 && parts[0] == "i" && parts[1] == "users" && r.Method == http.MethodDelete
	if !self && !verify && !internal {
		return false
	}
	if verify && r.Method != http.MethodPost {
		w.WriteHeader(405)
		return true
	}
	if internal {
		id, err := registrationUUID(parts[2])
		if err != nil {
			routeParseError(w, "Invalid user ID")
			return true
		}
		already, err := h.ensure(r.Context(), id, r.Header.Get("Request-Id"))
		if err != nil {
			accountDeletionHTTPError(w, err)
		} else if already {
			w.WriteHeader(200)
		} else {
			w.WriteHeader(202)
		}
		return true
	}
	var user gocql.UUID
	if self {
		var ok bool
		user, ok = userHeader(w, r)
		if !ok {
			return true
		}
	}
	if !publicClientContentType(w, r) {
		return true
	}
	raw, err := publicClientJSON(r)
	if err != nil {
		routeParseError(w, "Invalid deletion request")
		return true
	}
	fields, err := registrationObject(raw)
	if err != nil {
		routeParseError(w, "Invalid deletion request")
		return true
	}
	if self {
		password, err := registrationText(fields, "password", false)
		if err != nil || password != nil && validatePlainPassword(*password, 6) != nil {
			routeParseError(w, "Invalid deletion request")
			return true
		}
		ttl, err := h.service.self(r.Context(), user, password, r.Header.Get("Request-Id"))
		if err != nil {
			accountDeletionHTTPError(w, err)
		} else if ttl == nil {
			w.WriteHeader(200)
		} else {
			w.Header().Set("Content-Type", "application/json;charset=utf-8")
			w.WriteHeader(202)
			_ = json.NewEncoder(w).Encode(map[string]int64{"expires_in": *ttl})
		}
	} else {
		key, kerr := registrationText(fields, "key", true)
		code, cerr := registrationText(fields, "code", true)
		if kerr != nil || cerr != nil || len(*key) != 20 || len(*code) < 6 || len(*code) > 20 || !validActivationValue(*key) || !validActivationValue(*code) {
			routeParseError(w, "Invalid deletion verification request")
			return true
		}
		if err := h.verification.complete(r.Context(), *key, *code, r.Header.Get("Request-Id")); err != nil {
			accountDeletionHTTPError(w, err)
		} else {
			w.WriteHeader(200)
		}
	}
	return true
}

func (s connectionStore) deletionConnectionCount(ctx context.Context, user gocql.UUID) (int64, error) {
	var count int64
	for _, query := range []string{`SELECT status FROM connection WHERE left=?`, `SELECT status FROM connection_remote WHERE left=?`} {
		iter := s.cassandra.Query(query, user).WithContext(ctx).Consistency(gocql.One).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).Iter()
		var status *int32
		for iter.Scan(&status) {
			if status == nil || *status < 0 || *status > 11 {
				_ = iter.Close()
				return 0, errors.New("invalid deletion connection relation")
			}
			count++
		}
		if err := iter.Close(); err != nil {
			return 0, userStorageError(err)
		}
	}
	return count, nil
}
