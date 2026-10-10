// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"crypto/ed25519"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type memoryCookieLookup struct {
	store  *memoryCookieStore
	reads  int
	failOn int
}

func (s *memoryCookieLookup) lookup(_ context.Context, _ gocql.UUID, expires time.Time, id uint32) (*sessionCookie, error) {
	s.reads++
	if s.reads == s.failOn {
		return nil, errors.New("synthetic session failure")
	}
	for _, c := range s.store.cookies {
		if c.ID == id && c.Expires.Equal(expires) {
			return &c, nil
		}
	}
	return nil, nil
}

func TestAccessTokenValidationAndLogoutExpiredAccess(t *testing.T) {
	m, store, user := testSessionManager()
	ctx, created := context.Background(), time.Unix(1000, 0)
	cookie, err := m.newCookie(ctx, user, nil, 1, nil, false, false, created)
	if err != nil {
		t.Fatal(err)
	}
	access, _, err := m.issuer.access(cookie.token, nil, created)
	if err != nil {
		t.Fatal(err)
	}
	lookup := &memoryCookieLookup{store: store}
	s := accessTokenService{lookup: lookup, sessions: m, keys: []ed25519.PublicKey{m.issuer.key.Public().(ed25519.PublicKey)}, now: func() time.Time { return created.Add(100 * time.Second) }}
	credentials := accessCredentials{cookies: []ZToken{cookie.token}, access: &access}
	if _, selected, err := s.validate(ctx, credentials); err != nil || selected.record.ID != cookie.record.ID {
		t.Fatal("signed expired access must remain acceptable", err)
	}
	// Expired does not override a falsified signature.
	falsified := access
	falsified.Signature = append([]byte(nil), access.Signature...)
	falsified.Signature[0] ^= 1
	bad := credentials
	bad.access = &falsified
	if err := s.logout(ctx, bad); !errors.Is(err, ErrFalsifiedToken) || len(store.cookies) != 1 {
		t.Fatal("falsified token revoked session", err)
	}
	if err := s.logout(ctx, credentials); err != nil || len(store.cookies) != 0 {
		t.Fatal("logout did not revoke native session", err)
	}
	if _, _, err := s.validate(ctx, credentials); !errors.Is(err, ErrInvalidToken) {
		t.Fatal("revoked cookie still validates")
	}
}

func TestAccessMultipleCookiesFirstSuccessAndStorageFailure(t *testing.T) {
	m, store, user := testSessionManager()
	ctx, now := context.Background(), time.Unix(1000, 0)
	one, err := m.newCookie(ctx, user, nil, 1, nil, false, false, now)
	if err != nil {
		t.Fatal(err)
	}
	two, err := m.newCookie(ctx, user, nil, 1, nil, false, false, now)
	if err != nil {
		t.Fatal(err)
	}
	lookup := &memoryCookieLookup{store: store}
	s := accessTokenService{lookup: lookup, sessions: m, keys: []ed25519.PublicKey{m.issuer.key.Public().(ed25519.PublicKey)}, now: func() time.Time { return now }}
	expired := one.token
	expired.Expires.SetInt64(999)
	if err := expired.Sign(m.issuer.key); err != nil {
		t.Fatal(err)
	}
	credentials := accessCredentials{cookies: []ZToken{expired, two.token, one.token}}
	if _, selected, err := s.validate(ctx, credentials); err != nil || selected.record.ID != two.record.ID || lookup.reads != 2 {
		t.Fatal("must inspect all and select first valid cookie", err)
	}
	lookup.reads, lookup.failOn = 0, 2
	if _, _, err := s.validate(ctx, credentials); err == nil || errors.Is(err, ErrExpiredToken) {
		t.Fatal("storage failure must abort even after a valid cookie")
	}
	credentials.cookies = []ZToken{expired}
	if _, _, err := s.validate(ctx, credentials); !errors.Is(err, ErrExpiredToken) {
		t.Fatal("first token failure not retained")
	}
	access, _, err := m.issuer.access(two.token, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	access.User = "00000000-0000-0000-0000-000000000001"
	if err := access.Sign(m.issuer.key); err != nil {
		t.Fatal(err)
	}
	credentials.cookies, credentials.access = []ZToken{two.token}, &access
	if _, _, err := s.validate(ctx, credentials); !errors.Is(err, ErrInvalidToken) {
		t.Fatal("cross-user token accepted")
	}
}

func TestAccessCredentialPrecedenceCookieParsingAndKindPartition(t *testing.T) {
	m, _, user := testSessionManager()
	now := time.Unix(1000, 0)
	u, err := m.issuer.user(user, nil, 1, false, now)
	if err != nil {
		t.Fatal(err)
	}
	lu, err := m.issuer.user(user, nil, 1, true, now)
	if err != nil {
		t.Fatal(err)
	}
	a, _, err := m.issuer.access(u, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	la, _, err := m.issuer.access(lu, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	encode := func(token ZToken) string {
		value, err := token.Encode()
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	request := httptest.NewRequest("POST", "/access/logout?access_token="+encode(a), nil)
	request.Header.Set("Cookie", "other=x; zuid=invalid; zuid="+encode(u)+"; zuid="+encode(u))
	credentials, err := parseAccessCredentials(request)
	if err != nil || len(credentials.cookies) != 2 || credentials.access == nil {
		t.Fatal("valid duplicate cookies must survive bad cookie", err)
	}
	request.Header.Set("Authorization", "Basic invalid")
	if _, err := parseAccessCredentials(request); err == nil {
		t.Fatal("invalid header must take precedence over valid query")
	}
	request.Header.Set("Authorization", "Bearer "+encode(la))
	if _, err := parseAccessCredentials(request); err == nil {
		t.Fatal("mixed access/user kinds accepted")
	}
	request.Header.Del("Authorization")
	request.Header.Set("Cookie", "zuid="+encode(u)+"; zuid="+encode(lu))
	if _, err := parseAccessCredentials(request); err == nil {
		t.Fatal("mixed normal/legal hold cookies accepted")
	}
	request.Header.Set("Cookie", "zuid=invalid")
	if _, err := parseAccessCredentials(request); err == nil {
		t.Fatal("all malformed cookies ignored")
	}
}

func TestLogoutHTTPMissingCredentialsAndSecretSafeErrors(t *testing.T) {
	s := accessTokenService{}
	for _, test := range []struct {
		method, path, message string
		status                int
	}{
		{"POST", "/access/logout", "Missing cookie and token", 403},
		{"GET", "/access/logout", "", 405},
	} {
		response := httptest.NewRecorder()
		s.logoutHTTP(response, httptest.NewRequest(test.method, test.path, nil))
		if response.Code != test.status {
			t.Fatalf("logout status %d", response.Code)
		}
		if test.message != "" && response.Header().Get("Content-Type") != "application/json" {
			t.Fatal("native auth error media type changed")
		}
	}
	response := httptest.NewRecorder()
	tokenHTTPError(response, errors.New("synthetic private database value"))
	if response.Code != 500 || response.Body.String() != "{\"code\":500,\"label\":\"server-error\",\"message\":\"Internal Server Error\"}\n" {
		t.Fatal("private session errors leaked")
	}
}
