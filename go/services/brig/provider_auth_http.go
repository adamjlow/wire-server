// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type providerAuthHTTP struct {
	byEmail             func(context.Context, string) (*gocql.UUID, error)
	password            func(context.Context, gocql.UUID) (*string, error)
	write               func(context.Context, gocql.UUID, string) error
	verify              func(context.Context, passwordRateKey, string, string) (bool, bool, error)
	hash                func(context.Context, passwordRateKey, string, int) (string, error)
	secondFactorEnabled func(context.Context, string) (bool, error)
	issuer              tokenIssuer
	ttl                 int64
	cookieInsecure      bool
	now                 func() time.Time
}

// Provider credentials use the native six-character type for both passwords.
// Keep plaintext out of formatting and errors, as for user password changes.
type providerCredentials struct{ email, old, password string }

func (providerCredentials) String() string { return "<ProviderCredentials>" }

func decodeProviderCredentials(raw json.RawMessage, login bool) (providerCredentials, error) {
	var result providerCredentials
	fields, err := registrationObject(raw)
	if err != nil {
		return result, err
	}
	field := "new_password"
	if login {
		field = "password"
	}
	password, err := registrationText(fields, field, true)
	if err != nil || validatePlainPassword(*password, 6) != nil {
		return result, errors.New("invalid provider password")
	}
	result.password = *password
	if login {
		email, err := registrationText(fields, "email", true)
		if err != nil {
			return result, errors.New("invalid provider email")
		}
		result.email, err = canonicalRegistrationEmail(*email)
		return result, err
	}
	old, err := registrationText(fields, "old_password", true)
	if err != nil || validatePlainPassword(*old, 6) != nil {
		return result, errors.New("invalid provider password")
	}
	result.old = *old
	return result, nil
}

func (h providerAuthHTTP) check(ctx context.Context, id gocql.UUID, plain string) (bool, error) {
	if h.password == nil || h.verify == nil {
		return false, errors.New("provider authentication unavailable")
	}
	encoded, err := h.password(ctx, id)
	if err != nil {
		return false, err
	}
	if encoded == nil {
		return false, authInvalidCredentials
	}
	// Native callers discard the update status; verification must not write.
	valid, _, err := h.verify(ctx, passwordRateKey{kind: "provider", id: id.String()}, *encoded, plain)
	return valid, err
}

func (h providerAuthHTTP) change(ctx context.Context, id gocql.UUID, request providerCredentials) error {
	valid, err := h.check(ctx, id, request.old)
	if err != nil {
		return err
	}
	if !valid {
		return authInvalidCredentials
	}
	same, err := h.check(ctx, id, request.password)
	if err != nil {
		return err
	}
	if same {
		return errPasswordChangeSame
	}
	if h.hash == nil || h.write == nil {
		return errors.New("provider password update unavailable")
	}
	encoded, err := h.hash(ctx, passwordRateKey{kind: "provider", id: id.String()}, request.password, 6)
	if err != nil {
		return err
	}
	return h.write(ctx, id, encoded)
}

func (h providerAuthHTTP) handle(w http.ResponseWriter, r *http.Request, path string) bool {
	login := path == "/provider/login"
	if !login && path != "/provider/password" {
		return false
	}
	if login && r.Method != http.MethodPost || !login && r.Method != http.MethodPut {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return true
	}
	var id gocql.UUID
	if !login {
		var valid bool
		if id, valid = providerHeader(w, r); !valid {
			return true
		}
	}
	if !publicClientContentType(w, r) {
		return true
	}
	raw, err := publicClientJSON(r)
	var request providerCredentials
	if err == nil {
		request, err = decodeProviderCredentials(raw, login)
	}
	if err != nil {
		authenticationWireError(w, 400, "bad-request", "Invalid provider credentials")
		return true
	}
	if h.secondFactorEnabled == nil {
		authenticationHTTPError(w, errors.New("provider feature lookup unavailable"))
		return true
	}
	enabled, err := h.secondFactorEnabled(r.Context(), r.Header.Get("Request-Id"))
	if err != nil {
		authenticationHTTPError(w, err)
		return true
	}
	if enabled {
		authenticationWireError(w, 403, "access-denied", "Access denied.")
		return true
	}
	if !login {
		err = h.change(r.Context(), id, request)
		if errors.Is(err, errPasswordChangeSame) {
			authenticationWireError(w, 409, "password-must-differ", "For password reset, new and old password must be different.")
		} else if err != nil {
			authenticationHTTPError(w, err)
		} else {
			w.WriteHeader(http.StatusOK)
		}
		return true
	}
	if h.byEmail == nil || h.now == nil {
		authenticationHTTPError(w, errors.New("provider login unavailable"))
		return true
	}
	provider, err := h.byEmail(r.Context(), request.email)
	if err == nil && provider == nil {
		err = authInvalidCredentials
	}
	if err == nil {
		var valid bool
		valid, err = h.check(r.Context(), *provider, request.password)
		if err == nil && !valid {
			err = authInvalidCredentials
		}
	}
	if err != nil {
		authenticationHTTPError(w, err)
		return true
	}
	token, err := h.issuer.sign(ZToken{Kind: "p", Provider: provider.String()}, h.now(), h.ttl)
	if err != nil {
		authenticationHTTPError(w, err)
		return true
	}
	encoded, err := token.Encode()
	if err != nil {
		authenticationHTTPError(w, err)
		return true
	}
	http.SetCookie(w, &http.Cookie{Name: "zprovider", Value: encoded, Path: "/provider", Expires: time.Unix(token.Expires.Int64(), 0).UTC(), HttpOnly: true, Secure: !h.cookieInsecure})
	w.WriteHeader(http.StatusOK)
	return true
}
