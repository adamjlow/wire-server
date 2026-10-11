// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type loginHTTP struct {
	service          loginService
	syncIndex        func(context.Context, gocql.UUID, string) error
	refreshSuggested func(context.Context, gocql.UUID, string) error
	cookieInsecure   bool
}

func loginHTTPError(w http.ResponseWriter, err error) {
	var blocked loginBlocked
	var throttled cookieRetryAfter
	switch {
	case errors.As(err, &blocked):
		w.Header().Set("Retry-After", strconv.FormatInt(blocked.seconds, 10))
		authenticationWireError(w, 403, "client-error", "Too many failed logins")
	case errors.As(err, &throttled):
		w.Header().Set("Retry-After", strconv.FormatInt(throttled.seconds, 10))
		authenticationWireError(w, 429, "client-error", "Logins too frequent")
	case errors.Is(err, loginFailed):
		authenticationHTTPError(w, authInvalidCredentials)
	case errors.Is(err, loginPending):
		authenticationHTTPError(w, authPendingInvitation)
	case errors.Is(err, loginCodeRequired):
		authenticationWireError(w, 403, "code-authentication-required", "Verification code required.")
	case errors.Is(err, loginCodeInvalid):
		authenticationWireError(w, 403, "code-authentication-failed", "Code authentication failed (no such code).")
	default:
		authenticationHTTPError(w, err)
	}
}

func validLoginHandle(value string) bool {
	if len(value) < 2 || len(value) > 256 {
		return false
	}
	for _, ch := range value {
		if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-' || ch == '.') {
			return false
		}
	}
	return true
}

func decodeLogin(r io.Reader) (loginRequest, error) {
	invalid := errors.New("invalid login request")
	var body map[string]json.RawMessage
	decoder := json.NewDecoder(r)
	var trailing any
	if decoder.Decode(&body) != nil || body == nil || decoder.Decode(&trailing) != io.EOF {
		return loginRequest{}, invalid
	}
	optional := func(key string) (*string, error) {
		raw, exists := body[key]
		if !exists || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return nil, nil
		}
		var value string
		if !utf8.Valid(raw) || json.Unmarshal(raw, &value) != nil || !utf8.ValidString(value) {
			return nil, invalid
		}
		return &value, nil
	}
	email, err := optional("email")
	if err != nil {
		return loginRequest{}, err
	}
	handle, err := optional("handle")
	if err != nil {
		return loginRequest{}, err
	}
	// Parse both identifiers before choosing email, as native Schema LoginId
	// does; a malformed unused identifier cannot bypass server validation.
	if email != nil {
		parsed, err := mail.ParseAddress(*email)
		if err != nil || parsed.Address != *email || strings.ContainsAny(*email, "\r\n") {
			return loginRequest{}, invalid
		}
	}
	if handle != nil && !validLoginHandle(*handle) {
		return loginRequest{}, invalid
	}
	if email == nil && handle == nil {
		return loginRequest{}, invalid
	}
	password, err := optional("password")
	if err != nil || password == nil || validatePlainPassword(*password, 6) != nil {
		return loginRequest{}, invalid
	}
	label, err := optional("label")
	if err != nil {
		return loginRequest{}, err
	}
	code, err := optional("verification_code")
	if err != nil || code != nil && !validVerificationValue(*code) {
		return loginRequest{}, invalid
	}
	return loginRequest{identifier: loginIdentifier{email: email, handle: handle}, password: *password, label: label, code: code}, nil
}

func (h loginHTTP) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	persistent := false
	if values, exists := r.URL.Query()["persist"]; exists && len(values) > 0 {
		switch strings.ToLower(values[0]) {
		case "true":
			persistent = true
		case "false":
		default:
			routeParseError(w, "Error parsing query parameter persist failed: could not parse: `"+values[0]+"'")
			return
		}
	}
	if media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || !strings.EqualFold(media, "application/json") {
		w.WriteHeader(http.StatusUnsupportedMediaType)
		return
	}
	request, err := decodeLogin(r.Body)
	if err != nil {
		wireError(w, 400, "bad-request", "Invalid login request")
		return
	}
	request.persistent = persistent
	service := h.service
	requestID := r.Header.Get("Request-Id")
	if h.syncIndex != nil {
		service.lifecycle.syncIndex = func(ctx context.Context, user gocql.UUID) error { return h.syncIndex(ctx, user, requestID) }
	}
	if h.refreshSuggested != nil {
		service.sessions.refreshSuggested = func(ctx context.Context, user gocql.UUID) error { return h.refreshSuggested(ctx, user, requestID) }
	}
	access, err := service.login(r.Context(), request, requestID)
	if err != nil {
		loginHTTPError(w, err)
		return
	}
	if err := writeAccessResponse(w, access, h.cookieInsecure); err != nil {
		loginHTTPError(w, err)
	}
}
