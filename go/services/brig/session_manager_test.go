// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"crypto/ed25519"
	"errors"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type memoryCookieStore struct {
	cookies []sessionCookie
	ttls    []*int32
	writes  int
}

func (s *memoryCookieStore) insert(_ context.Context, _ gocql.UUID, c sessionCookie, ttl *int32) error {
	s.writes++
	s.ttls = append(s.ttls, ttl)
	for n, old := range s.cookies {
		if old.ID == c.ID && old.Expires.Equal(c.Expires) {
			s.cookies[n] = c
			return nil
		}
	}
	s.cookies = append(s.cookies, c)
	return nil
}
func (s *memoryCookieStore) list(context.Context, gocql.UUID) ([]sessionCookie, error) {
	return append([]sessionCookie(nil), s.cookies...), nil
}
func (s *memoryCookieStore) delete(_ context.Context, _ gocql.UUID, selected []sessionCookie) error {
	remaining := []sessionCookie{}
	for _, c := range s.cookies {
		remove := false
		for _, d := range selected {
			if c.ID == d.ID && c.Expires.Equal(d.Expires) {
				remove = true
			}
		}
		if !remove {
			remaining = append(remaining, c)
		}
	}
	s.cookies = remaining
	return nil
}
func (s *memoryCookieStore) deleteAll(context.Context, gocql.UUID) error { s.cookies = nil; return nil }

func testSessionManager() (sessionManager, *memoryCookieStore, gocql.UUID) {
	store := &memoryCookieStore{}
	issuer := tokenIssuer{key: ed25519.NewKeyFromSeed(make([]byte, 32)), index: 1, userTTL: 1000, sessionTTL: 200, accessTTL: 30, legalUserTTL: 2000, legalAccessTTL: 60}
	return sessionManager{store: store, issuer: issuer, renewAge: 100, limit: 2, throttle: cookieThrottle{stdDev: 1, retryAfter: 10}}, store, gocql.UUID{}
}

func TestSessionRenewalBindingOverlapAndSuccessor(t *testing.T) {
	m, store, user := testSessionManager()
	ctx, now := context.Background(), time.Unix(1000, 250000000)
	old, err := m.newCookie(ctx, user, nil, 1, nil, false, false, now)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := m.nextCookie(ctx, old, nil, now.Add(100*time.Second)); err != nil || got != nil {
		t.Fatal("renewal equality must retain original")
	}
	client := "abc"
	newCookie, err := m.nextCookie(ctx, old, &client, now.Add(time.Second))
	if err != nil || newCookie == nil {
		t.Fatal("new client must renew regardless of age", err)
	}
	if newCookie.token.Client == nil || *newCookie.token.Client != client || len(store.cookies) != 2 {
		t.Fatal("renewal failed client binding or overlap")
	}
	linked := store.cookies[0]
	if linked.Successor == nil || *linked.Successor != newCookie.record.ID || store.ttls[2] == nil || *store.ttls[2] != 100 {
		t.Fatal("old cookie must link successor with renewal TTL")
	}
	old.record = linked
	// A later overlapping request re-signs the same successor, not another row.
	again, err := m.nextCookie(ctx, old, &client, now.Add(2*time.Second))
	if err != nil || again == nil || again.record.ID != newCookie.record.ID || store.writes != 3 {
		t.Fatal("overlap should reuse stored successor", err)
	}
	other := "def"
	if _, err := m.nextCookie(ctx, *newCookie, &other, now.Add(101*time.Second)); !errors.Is(err, ErrInvalidToken) || store.writes != 3 {
		t.Fatal("mismatched client must not write")
	}
	// A session cookie renews only to acquire a previously absent client.
	session, err := m.newCookie(ctx, user, nil, 0, nil, false, false, now)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := m.nextCookie(ctx, session, nil, now.Add(150*time.Second)); err != nil || got != nil {
		t.Fatal("session age must not renew")
	}
	if got, err := m.nextCookie(ctx, session, &client, now.Add(time.Second)); err != nil || got == nil || !got.token.Session {
		t.Fatal("session client binding must keep session token", err)
	}
}

func TestSessionLimitsLabelsAndRevocation(t *testing.T) {
	m, store, user := testSessionManager()
	ctx, now := context.Background(), time.Unix(1000, 0)
	label := "phone"
	events := 0
	m.refreshSuggested = func(context.Context, gocql.UUID) error { events++; return nil }
	first, err := m.newCookie(ctx, user, nil, 1, &label, false, true, now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := m.newCookie(ctx, user, nil, 1, &label, false, true, now)
	if err != nil || events != 1 || len(store.cookies) != 1 || store.cookies[0].ID != second.record.ID || first.record.ID == second.record.ID {
		t.Fatal("same-label replacement must retain new row and send one event", err)
	}
	if _, err := m.newCookie(ctx, user, nil, 1, nil, false, false, now); err != nil {
		t.Fatal(err)
	}
	before := store.writes
	_, err = m.limitedCookie(ctx, user, nil, 1, nil, false, false, now.Add(3*time.Second))
	var wait cookieRetryAfter
	if !errors.As(err, &wait) || wait.seconds != 7 || store.writes != before || len(store.cookies) != 2 {
		t.Fatal("throttled issuance must leave sessions intact", err)
	}
	// Persistent count cannot consume the separately applied session limit.
	if _, err := m.limitedCookie(ctx, user, nil, 0, nil, false, false, now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := m.limitedCookie(ctx, user, nil, 1, nil, false, false, now.Add(10*time.Second)); err != nil || len(store.cookies) != 3 {
		t.Fatal("penalty expiry must evict one persistent cookie", err)
	}
	if _, err := m.revoke(ctx, user, nil, nil, nil); err != nil || len(store.cookies) != 0 {
		t.Fatal("empty selection must revoke all", err)
	}
}

func TestTokenIssuanceAndAccessRenewal(t *testing.T) {
	m, _, user := testSessionManager()
	now := time.Unix(1000, 999999999)
	client, other := "abc", "def"
	for _, legal := range []bool{false, true} {
		cookie, err := m.issuer.user(user, &client, 1, legal, now)
		if err != nil {
			t.Fatal(err)
		}
		access, ttl, err := m.issuer.access(cookie, nil, now)
		if err != nil {
			t.Fatal(err)
		}
		keys := []ed25519.PublicKey{m.issuer.key.Public().(ed25519.PublicKey)}
		if err := access.Verify(keys, now); err != nil || access.Expires.Int64() != 1000+ttl {
			t.Fatal("issued token signature or fractional expiration changed", err)
		}
		cookie.Client = &other
		renewed, _, err := m.issuer.access(cookie, &access, now.Add(time.Second))
		if err != nil || renewed.Connection != access.Connection {
			t.Fatal("renewal must preserve connection", err)
		}
		if access.Expires.Int64() != 1000+ttl || access.Verify(keys, now) != nil {
			t.Fatal("renewal mutated the original signed token")
		}
		want := other
		if legal {
			want = client
		}
		if renewed.Client == nil || *renewed.Client != want {
			t.Fatal("normal renewal replaces client; legal hold preserves it")
		}
	}
	if _, err := m.issuer.user(user, nil, 0, true, now); !errors.Is(err, ErrInvalidToken) {
		t.Fatal("legal hold session cookies unsupported")
	}
}

func TestCookieThrottleNativeRoundingAndSampleDeviation(t *testing.T) {
	throttle := cookieThrottle{stdDev: 1, retryAfter: 10}
	cookies := []sessionCookie{{Created: time.Unix(1000, 500000000)}}
	if got := cookieThrottleWait(time.Unix(1002, 500000000), throttle, cookies); got != 8 {
		t.Fatalf("ties-to-even wait: %d", got)
	}
	cookies = append(cookies, sessionCookie{Created: time.Unix(1002, 0)})
	// Sample SD sqrt(2) exceeds1; population SD1 would incorrectly throttle.
	if got := cookieThrottleWait(time.Unix(1002, 0), throttle, cookies); got != 0 {
		t.Fatal("sample deviation must allow spaced issuance")
	}
}
