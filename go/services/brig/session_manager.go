// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"math"
	"slices"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

// This boundary is implemented by the native-schema Cassandra session store.
type cookieStorage interface {
	insert(context.Context, gocql.UUID, sessionCookie, *int32) error
	list(context.Context, gocql.UUID) ([]sessionCookie, error)
	delete(context.Context, gocql.UUID, []sessionCookie) error
	deleteAll(context.Context, gocql.UUID) error
}

type tokenIssuer struct {
	key                                                          ed25519.PrivateKey
	index                                                        int64
	userTTL, sessionTTL, accessTTL, legalUserTTL, legalAccessTTL int64
}

func (i tokenIssuer) sign(t ZToken, now time.Time, ttl int64) (ZToken, error) {
	t.Version, t.Key = 1, i.index
	// floor(now + integer TTL), as Data.ZAuth.Creation.newToken does.
	t.Expires.SetInt64(now.Unix() + ttl)
	if err := t.Sign(i.key); err != nil {
		return ZToken{}, err
	}
	return t, nil
}

func (i tokenIssuer) user(user gocql.UUID, client *string, typ int32, legal bool, now time.Time) (ZToken, error) {
	if typ != 0 && typ != 1 || legal && typ == 0 {
		return ZToken{}, ErrInvalidToken
	}
	var random [4]byte
	if _, err := rand.Read(random[:]); err != nil {
		return ZToken{}, errors.New("token randomness unavailable")
	}
	t := ZToken{Kind: "u", User: user.String(), Client: client, Random: binary.BigEndian.Uint32(random[:]), Session: typ == 0}
	ttl := i.userTTL
	if legal {
		t.Kind, ttl = "lu", i.legalUserTTL
	} else if typ == 0 {
		ttl = i.sessionTTL
	}
	return i.sign(t, now, ttl)
}

func (i tokenIssuer) access(cookie ZToken, previous *ZToken, now time.Time) (ZToken, int64, error) {
	kind, ttl := "a", i.accessTTL
	if cookie.Kind == "lu" {
		kind, ttl = "la", i.legalAccessTTL
	} else if cookie.Kind != "u" {
		return ZToken{}, 0, ErrInvalidToken
	}
	t := ZToken{Kind: kind, User: cookie.User, Client: cookie.Client}
	if previous == nil {
		var random [8]byte
		if _, err := rand.Read(random[:]); err != nil {
			return ZToken{}, 0, errors.New("token randomness unavailable")
		}
		t.Connection = binary.BigEndian.Uint64(random[:])
	} else {
		if previous.Kind != kind || previous.User != cookie.User {
			return ZToken{}, 0, ErrInvalidToken
		}
		t = *previous
		if kind == "a" {
			t.Client = cookie.Client
		}
	}
	signed, err := i.sign(t, now, ttl)
	return signed, ttl, err
}

type issuedCookie struct {
	record sessionCookie
	token  ZToken
}

type cookieThrottle struct {
	stdDev     float64
	retryAfter int64
}

type cookieRetryAfter struct{ seconds int64 }

func (cookieRetryAfter) Error() string { return "cookie issuance throttled" }

// Native uses ties-to-even POSIX rounding and sample (n-1) deviation, with
// singleton deviation zero. Called only when a nonempty list needs eviction.
func cookieThrottleWait(now time.Time, throttle cookieThrottle, cookies []sessionCookie) int64 {
	if len(cookies) == 0 {
		return 0
	}
	seconds := func(t time.Time) float64 {
		return math.RoundToEven(float64(t.Unix()) + float64(t.Nanosecond())/1e9)
	}
	latest, sum := seconds(cookies[0].Created), 0.0
	for _, c := range cookies {
		x := seconds(c.Created)
		latest = max(latest, x)
		sum += x
	}
	mean, variance := sum/float64(len(cookies)), 0.0
	for _, c := range cookies {
		d := seconds(c.Created) - mean
		variance += d * d
	}
	deviation := 0.0
	if len(cookies) > 1 {
		deviation = math.Sqrt(variance / float64(len(cookies)-1))
	}
	delta := int64(seconds(now)) - int64(latest)
	if deviation > throttle.stdDev || delta >= throttle.retryAfter {
		return 0
	}
	return throttle.retryAfter - delta
}

type sessionManager struct {
	store    cookieStorage
	issuer   tokenIssuer
	renewAge int64
	limit    int
	throttle cookieThrottle
	// The caller supplies the native UserSessionRefreshSuggested event path.
	refreshSuggested func(context.Context, gocql.UUID) error
}

func (m sessionManager) revoke(ctx context.Context, user gocql.UUID, except *uint32, ids []uint32, labels []string) (bool, error) {
	if except == nil && len(ids) == 0 && len(labels) == 0 {
		return true, m.store.deleteAll(ctx, user)
	}
	cookies, err := m.store.list(ctx, user)
	if err != nil {
		return false, err
	}
	selected := []sessionCookie{}
	for _, c := range cookies {
		if except != nil && c.ID == *except {
			continue
		}
		if slices.Contains(ids, c.ID) || c.Label != nil && slices.Contains(labels, *c.Label) {
			selected = append(selected, c)
		}
	}
	return len(selected) != 0, m.store.delete(ctx, user, selected)
}

func (m sessionManager) newCookie(ctx context.Context, user gocql.UUID, client *string, typ int32, label *string, legal, revokeSameLabel bool, now time.Time) (issuedCookie, error) {
	// Fail before writing if a required event dependency was omitted.
	if revokeSameLabel && label != nil && m.refreshSuggested == nil {
		return issuedCookie{}, errors.New("session refresh event unavailable")
	}
	token, err := m.issuer.user(user, client, typ, legal, now)
	if err != nil {
		return issuedCookie{}, err
	}
	c := issuedCookie{token: token, record: sessionCookie{ID: token.Random, Type: typ, Created: now, Expires: time.Unix(token.Expires.Int64(), 0).UTC(), Label: label}}
	if err := m.store.insert(ctx, user, c.record, nil); err != nil {
		return issuedCookie{}, err
	}
	if revokeSameLabel && label != nil {
		revoked, err := m.revoke(ctx, user, &c.record.ID, nil, []string{*label})
		if err != nil {
			return issuedCookie{}, err
		}
		if revoked {
			if err := m.refreshSuggested(ctx, user); err != nil {
				return issuedCookie{}, err
			}
		}
	}
	return c, nil
}

func (m sessionManager) limitedCookie(ctx context.Context, user gocql.UUID, client *string, typ int32, label *string, legal, revokeSameLabel bool, now time.Time) (issuedCookie, error) {
	cookies, err := m.store.list(ctx, user)
	if err != nil {
		return issuedCookie{}, err
	}
	sameType := []sessionCookie{}
	for _, c := range cookies {
		if c.Type == typ {
			sameType = append(sameType, c)
		}
	}
	evict := cookiesToEvict(m.limit, now, sameType)
	if len(evict) != 0 {
		if wait := cookieThrottleWait(now, m.throttle, sameType); wait != 0 {
			return issuedCookie{}, cookieRetryAfter{wait}
		}
		ids := make([]uint32, len(evict))
		for n, c := range evict {
			ids[n] = c.ID
		}
		if _, err := m.revoke(ctx, user, nil, ids, nil); err != nil {
			return issuedCookie{}, err
		}
	}
	return m.newCookie(ctx, user, client, typ, label, legal, revokeSameLabel, now)
}

// nextCookie is called after signature, expiry, account and stored-cookie
// validation. Nil means no replacement; the original cookie remains valid.
func (m sessionManager) nextCookie(ctx context.Context, old issuedCookie, client *string, now time.Time) (*issuedCookie, error) {
	previous := old.token.Client
	if previous != nil && client != nil && *previous != *client {
		return nil, ErrInvalidToken
	}
	if previous != nil {
		client = previous
	}
	changed := previous == nil && client != nil
	if !changed && (old.record.Type != 1 || now.Sub(old.record.Created).Seconds() <= float64(m.renewAge)) {
		return nil, nil
	}
	user, err := gocql.ParseUUID(old.token.User)
	if err != nil {
		return nil, ErrInvalidToken
	}
	if old.record.Successor != nil {
		cookies, err := m.store.list(ctx, user)
		if err != nil {
			return nil, err
		}
		for _, c := range cookies {
			if c.ID == *old.record.Successor && c.Type == 1 {
				t := ZToken{Version: 1, Key: m.issuer.index, Kind: old.token.Kind, User: old.token.User, Client: client, Random: c.ID}
				t.Expires.SetInt64(c.Expires.Unix())
				if err := t.Sign(m.issuer.key); err != nil {
					return nil, err
				}
				return &issuedCookie{record: c, token: t}, nil
			}
		}
	}
	newCookie, err := m.newCookie(ctx, user, client, old.record.Type, old.record.Label, old.token.Kind == "lu", false, now)
	if err != nil {
		return nil, err
	}
	linked := old.record
	linked.Successor = &newCookie.record.ID
	ttl := int32(m.renewAge)
	if err := m.store.insert(ctx, user, linked, &ttl); err != nil {
		return nil, err
	}
	return &newCookie, nil
}
