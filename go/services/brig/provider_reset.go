// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
)

var errProviderVerificationCode = errors.New("invalid provider verification code")

type providerResetPending struct{ ttl int32 }

func (providerResetPending) Error() string { return "provider reset pending" }

type providerResetService struct {
	auth           providerAuthHTTP
	lookup         func(context.Context, string, int32) (*providerVerificationCode, error)
	insert         func(context.Context, int32, providerVerificationCode) error
	remove         func(context.Context, string, int32) error
	throttle       func(context.Context, string, int32) (*int32, error)
	insertThrottle func(context.Context, string, int32, int32) error
	mail           func(context.Context, string, string, string) error
	newValue       func() (string, error)
	throttleTTL    int32
}

func newProviderCodeValue() (string, error) {
	var random [15]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", errors.New("provider code generation failed")
	}
	return base64.URLEncoding.EncodeToString(random[:]), nil
}

func (s providerResetService) begin(ctx context.Context, email string) error {
	if s.auth.byEmail == nil || s.lookup == nil || s.newValue == nil || s.throttle == nil || s.insertThrottle == nil || s.insert == nil || s.mail == nil {
		return errors.New("provider reset unavailable")
	}
	id, err := s.auth.byEmail(ctx, email)
	if err != nil {
		return err
	}
	if id == nil {
		return authInvalidCredentials
	}
	key, err := emailVerificationKey(email)
	if err != nil {
		return err
	}
	code, err := s.lookup(ctx, key, providerResetScope)
	if err != nil {
		return err
	}
	if code != nil {
		return providerResetPending{code.ttl}
	}
	value, err := s.newValue()
	if err != nil {
		return err
	}
	delay, err := s.throttle(ctx, key, providerResetScope)
	if err != nil {
		return err
	}
	if delay != nil {
		return deletionCodeThrottled{*delay}
	}
	if err := s.insertThrottle(ctx, key, providerResetScope, s.throttleTTL); err != nil {
		return err
	}
	code = &providerVerificationCode{key: key, value: value, email: email, account: id, retries: 3, ttl: 3600}
	if err := s.insert(ctx, providerResetScope, *code); err != nil {
		return err
	}
	// Native beginPasswordReset retains its code and throttle if delivery fails.
	return s.mail(ctx, email, key, value)
}

func (s providerResetService) verify(ctx context.Context, key, value string) (*providerVerificationCode, error) {
	if s.lookup == nil || s.insert == nil {
		return nil, errors.New("provider verification unavailable")
	}
	code, err := s.lookup(ctx, key, providerResetScope)
	if err != nil || code == nil {
		return nil, err
	}
	if code.retries == 0 {
		return nil, nil
	}
	if subtle.ConstantTimeCompare([]byte(code.value), []byte(value)) == 1 {
		return code, nil
	}
	updated := *code
	updated.retries--
	if err := s.insert(ctx, providerResetScope, updated); err != nil {
		return nil, err
	}
	return nil, nil
}

func (s providerResetService) complete(ctx context.Context, request providerResetRequest) error {
	code, err := s.verify(ctx, request.key, request.code)
	if err != nil {
		return err
	}
	if code == nil {
		return errProviderVerificationCode
	}
	if code.account == nil {
		return errPasswordResetCode
	}
	same, err := s.auth.check(ctx, *code.account, request.password)
	if err != nil {
		return err
	}
	if same {
		return errPasswordResetSame
	}
	if s.auth.hash == nil || s.auth.write == nil || s.remove == nil {
		return errors.New("provider reset mutation unavailable")
	}
	encoded, err := s.auth.hash(ctx, passwordRateKey{kind: "provider", id: code.account.String()}, request.password, 6)
	if err != nil {
		return err
	}
	if err := s.auth.write(ctx, *code.account, encoded); err != nil {
		return err
	}
	return s.remove(ctx, request.key, providerResetScope)
}

type providerResetRequest struct{ email, key, code, password string }

func (providerResetRequest) String() string { return "<ProviderResetRequest>" }

func decodeProviderReset(raw json.RawMessage, complete bool) (providerResetRequest, error) {
	var result providerResetRequest
	fields, err := registrationObject(raw)
	if err != nil {
		return result, err
	}
	if !complete {
		email, err := registrationText(fields, "email", true)
		if err != nil {
			return result, err
		}
		result.email, err = canonicalRegistrationEmail(*email)
		return result, err
	}
	for _, field := range []struct {
		name string
		into *string
	}{{"key", &result.key}, {"code", &result.code}, {"password", &result.password}} {
		value, err := registrationText(fields, field.name, true)
		if err != nil {
			return result, err
		}
		*field.into = *value
	}
	if len(result.key) != 20 || !validActivationValue(result.key) || !validVerificationValue(result.code) || validatePlainPassword(result.password, 6) != nil {
		return result, errors.New("invalid provider reset request")
	}
	return result, nil
}

func providerResetHTTPError(w http.ResponseWriter, err error) {
	var pending providerResetPending
	var throttle deletionCodeThrottled
	switch {
	case errors.As(err, &pending):
		w.Header().Set("Retry-After", strconv.FormatInt(int64(pending.ttl), 10))
		// Native RichError supplies Retry-After but no Content-Type header.
		w.Header()["Content-Type"] = nil
		w.WriteHeader(409)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 409, "label": "code-exists", "message": "A password reset is already in progress."})
	case errors.As(err, &throttle):
		w.Header().Set("Retry-After", strconv.FormatInt(int64(throttle.ttl), 10))
		w.Header()["Content-Type"] = nil
		w.WriteHeader(429)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 429, "label": "too-many-requests", "message": "Too many request to generate a verification code."})
	case errors.Is(err, errProviderVerificationCode):
		authenticationWireError(w, 403, "invalid-code", "Invalid verification code")
	case errors.Is(err, errPasswordResetCode), errors.Is(err, errPasswordResetSame):
		passwordResetHTTPError(w, err)
	default:
		authenticationHTTPError(w, err)
	}
}

func (s providerResetService) handle(w http.ResponseWriter, r *http.Request, path string) bool {
	internal := path == "/i/provider/password-reset-code"
	complete := path == "/provider/password-reset/complete"
	if !internal && !complete && path != "/provider/password-reset" {
		return false
	}
	if internal {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(405)
			return true
		}
		email, err := canonicalRegistrationEmail(r.URL.Query().Get("email"))
		if err != nil {
			routeParseError(w, "Invalid provider email")
			return true
		}
		key, _ := emailVerificationKey(email)
		if s.lookup == nil {
			providerResetHTTPError(w, errors.New("provider code lookup unavailable"))
			return true
		}
		code, err := s.lookup(r.Context(), key, providerResetScope)
		if err != nil {
			providerResetHTTPError(w, err)
		} else if code == nil {
			authenticationWireError(w, 404, "not-found", "Password reset key not found.")
		} else {
			readJSON(w, r, map[string]string{"key": key, "code": code.value})
		}
		return true
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return true
	}
	if !publicClientContentType(w, r) {
		return true
	}
	raw, err := publicClientJSON(r)
	var request providerResetRequest
	if err == nil {
		request, err = decodeProviderReset(raw, complete)
	}
	if err != nil {
		authenticationWireError(w, 400, "bad-request", "Invalid provider reset request")
		return true
	}
	if s.auth.secondFactorEnabled == nil {
		providerResetHTTPError(w, errors.New("provider feature lookup unavailable"))
		return true
	}
	enabled, err := s.auth.secondFactorEnabled(r.Context(), r.Header.Get("Request-Id"))
	if err != nil {
		providerResetHTTPError(w, err)
		return true
	}
	if enabled {
		authenticationWireError(w, 403, "access-denied", "Access denied.")
		return true
	}
	status := 201
	if complete {
		err, status = s.complete(r.Context(), request), 200
	} else {
		err = s.begin(r.Context(), request.email)
	}
	if err != nil {
		providerResetHTTPError(w, err)
	} else {
		w.WriteHeader(status)
	}
	return true
}
