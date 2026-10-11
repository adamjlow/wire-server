// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"crypto/ed25519"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type cookieLookup interface {
	lookup(context.Context, gocql.UUID, time.Time, uint32) (*sessionCookie, error)
}

type accessCredentials struct {
	cookies []ZToken
	access  *ZToken
}

type tokenHTTPFailure struct{ label, message string }

func (tokenHTTPFailure) Error() string { return "token credentials rejected" }

func tokenCredentialError(message string) error {
	return tokenHTTPFailure{"invalid-credentials", message}
}

func parseSomeToken(value string, access bool) (ZToken, error) {
	kinds := []string{"u", "lu"}
	if access {
		kinds = []string{"a", "la"}
	}
	for _, kind := range kinds {
		if t, err := ParseZToken(value, kind); err == nil {
			return t, nil
		}
	}
	return ZToken{}, tokenHTTPFailure{"client-error", "Failed reading: Invalid zauth token"}
}

func parseAccessCredentials(r *http.Request) (accessCredentials, error) {
	var credentials accessCredentials
	var firstCookieError error
	// Native CookieHeader reads the first header and retains all duplicate
	// zuid values in their original order. Bad cookies are ignored if any parse.
	if headers := r.Header["Cookie"]; len(headers) != 0 {
		for _, piece := range strings.Split(headers[0], ";") {
			name, value, ok := strings.Cut(strings.TrimSpace(piece), "=")
			if !ok || name != "zuid" {
				continue
			}
			t, err := parseSomeToken(value, false)
			if err == nil {
				credentials.cookies = append(credentials.cookies, t)
			} else if firstCookieError == nil {
				firstCookieError = err
			}
		}
	}
	if len(credentials.cookies) == 0 && firstCookieError != nil {
		return credentials, firstCookieError
	}
	var raw *string
	if headers := r.Header["Authorization"]; len(headers) != 0 {
		if !strings.HasPrefix(headers[0], "Bearer ") {
			return credentials, tokenCredentialError("Invalid authorization scheme")
		}
		value := strings.TrimPrefix(headers[0], "Bearer ")
		raw = &value
	} else if query, exists := r.URL.Query()["access_token"]; exists && len(query) != 0 {
		raw = &query[0]
	}
	if raw != nil {
		t, err := parseSomeToken(*raw, true)
		if err != nil {
			return credentials, err
		}
		credentials.access = &t
	}
	if len(credentials.cookies) == 0 {
		if credentials.access == nil {
			return credentials, tokenCredentialError("Missing cookie and token")
		}
		return credentials, tokenCredentialError("Missing cookie")
	}
	kind := credentials.cookies[0].Kind
	for _, cookie := range credentials.cookies {
		if cookie.Kind != kind {
			return credentials, tokenCredentialError("Token mismatch")
		}
	}
	if credentials.access != nil && (kind == "u" && credentials.access.Kind != "a" || kind == "lu" && credentials.access.Kind != "la") {
		return credentials, tokenCredentialError("Token mismatch")
	}
	return credentials, nil
}

type accessTokenService struct {
	lookup   cookieLookup
	sessions sessionManager
	keys     []ed25519.PublicKey
	now      func() time.Time
}

type clientLookup interface {
	get(context.Context, gocql.UUID, string) (*storedClient, error)
}

type accessRenewalService struct {
	tokens          accessTokenService
	clients         clientLookup
	accounts        accountLifecycle
	inactiveTimeout *time.Duration
}

type renewedAccess struct {
	token     ZToken
	expiresIn int64
	cookie    *issuedCookie
}

// Renewal requires a live stored cookie, then an optional real client, then
// inactivity enforcement. Expired signed access tokens may still be renewed.
func (s accessRenewalService) renew(ctx context.Context, credentials accessCredentials, client *string) (renewedAccess, error) {
	user, old, err := s.tokens.validate(ctx, credentials)
	if err != nil {
		return renewedAccess{}, err
	}
	if client != nil {
		found, err := s.clients.get(ctx, user, *client)
		if err != nil {
			return renewedAccess{}, err
		}
		if found == nil {
			return renewedAccess{}, ErrInvalidToken
		}
	}
	if err := s.accounts.suspendInactive(ctx, user, s.inactiveTimeout, s.tokens.now()); err != nil {
		return renewedAccess{}, err
	}
	next, err := s.tokens.sessions.nextCookie(ctx, old, client, s.tokens.now())
	if err != nil {
		return renewedAccess{}, err
	}
	cookie := old.token
	if next != nil {
		cookie = next.token
	}
	token, ttl, err := s.tokens.sessions.issuer.access(cookie, credentials.access, s.tokens.now())
	return renewedAccess{token: token, expiresIn: ttl, cookie: next}, err
}

func (s accessTokenService) validateOne(ctx context.Context, token ZToken, access *ZToken, now time.Time) (gocql.UUID, issuedCookie, error) {
	if access != nil && access.User != token.User {
		return gocql.UUID{}, issuedCookie{}, ErrInvalidToken
	}
	if err := token.Verify(s.keys, now); err != nil {
		return gocql.UUID{}, issuedCookie{}, err
	}
	if access != nil {
		if err := access.Verify(s.keys, now); err != nil && !errors.Is(err, ErrExpiredToken) {
			return gocql.UUID{}, issuedCookie{}, err
		}
	}
	user, err := gocql.ParseUUID(token.User)
	if err != nil || !token.Expires.IsInt64() {
		return gocql.UUID{}, issuedCookie{}, ErrInvalidToken
	}
	cookie, err := s.lookup.lookup(ctx, user, time.Unix(token.Expires.Int64(), 0).UTC(), token.Random)
	if err != nil {
		return gocql.UUID{}, issuedCookie{}, err
	}
	if cookie == nil {
		return gocql.UUID{}, issuedCookie{}, ErrInvalidToken
	}
	return user, issuedCookie{record: *cookie, token: token}, nil
}

func (s accessTokenService) validate(ctx context.Context, credentials accessCredentials) (gocql.UUID, issuedCookie, error) {
	now := s.now()
	var firstFailure error
	var selected *issuedCookie
	var selectedUser gocql.UUID
	// Native traverses every supplied parsed token and then chooses the first
	// success, or the first token failure if none. Storage failures still abort.
	for _, token := range credentials.cookies {
		user, cookie, err := s.validateOne(ctx, token, credentials.access, now)
		if err != nil {
			if !errors.Is(err, ErrInvalidToken) && !errors.Is(err, ErrFalsifiedToken) && !errors.Is(err, ErrExpiredToken) {
				return gocql.UUID{}, issuedCookie{}, err
			}
			if firstFailure == nil {
				firstFailure = err
			}
		} else if selected == nil {
			selected = &cookie
			selectedUser = user
		}
	}
	if selected != nil {
		return selectedUser, *selected, nil
	}
	if firstFailure == nil {
		firstFailure = ErrInvalidToken
	}
	return gocql.UUID{}, issuedCookie{}, firstFailure
}

func (s accessTokenService) logout(ctx context.Context, credentials accessCredentials) error {
	if credentials.access == nil {
		return tokenCredentialError("Missing token")
	}
	user, cookie, err := s.validate(ctx, credentials)
	if err != nil {
		return err
	}
	_, err = s.sessions.revoke(ctx, user, nil, []uint32{cookie.record.ID}, nil)
	return err
}

func tokenHTTPError(w http.ResponseWriter, err error) {
	var failure tokenHTTPFailure
	switch {
	case errors.As(err, &failure):
		authenticationWireError(w, 403, failure.label, failure.message)
	case errors.Is(err, ErrExpiredToken):
		authenticationWireError(w, 403, "invalid-credentials", "Zauth token expired")
	case errors.Is(err, ErrFalsifiedToken):
		authenticationWireError(w, 403, "invalid-credentials", "Zauth token falsified")
	case errors.Is(err, ErrInvalidToken):
		authenticationWireError(w, 403, "invalid-credentials", "Invalid zauth token")
	default:
		authenticationWireError(w, 500, "server-error", "Internal Server Error")
	}
}

func (s accessTokenService) logoutHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	credentials, err := parseAccessCredentials(r)
	if err == nil {
		err = s.logout(r.Context(), credentials)
	}
	if err != nil {
		tokenHTTPError(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}
