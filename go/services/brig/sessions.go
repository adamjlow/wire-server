// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"
	"math"
	"sort"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

// Cookie records always remain in Cassandra, including when the configured
// user store is PostgreSQL. No token secret is persisted in these records.
type sessionCookie struct {
	ID        uint32
	Type      int32
	Created   time.Time
	Expires   time.Time
	Label     *string
	Successor *uint32
}

type sessionStore struct{ cassandra *gocql.Session }

func (s sessionStore) insert(ctx context.Context, user gocql.UUID, cookie sessionCookie, overrideTTL *int32) error {
	// Haskell's round uses ties-to-even; time.Duration's truncation would change
	// renewal overlap TTL at half-second boundaries.
	ttl := int32(math.RoundToEven(cookie.Expires.Sub(cookie.Created).Seconds()))
	if overrideTTL != nil {
		ttl = *overrideTTL
	}
	var successor *int64
	if cookie.Successor != nil {
		x := int64(*cookie.Successor)
		successor = &x
	}
	err := s.cassandra.Query(`INSERT INTO user_cookies (user,expires,id,type,created,label,succ_id) VALUES (?,?,?,?,?,?,?) USING TTL ?`,
		user, cookie.Expires, int64(cookie.ID), cookie.Type, cookie.Created, cookie.Label, successor, ttl).
		WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5}).Exec()
	return sessionError(err)
}

func sessionError(err error) error {
	if err != nil {
		return errors.New("session storage operation failed")
	}
	return nil
}

func (s sessionStore) lookup(ctx context.Context, user gocql.UUID, expires time.Time, id uint32) (*sessionCookie, error) {
	cookie := sessionCookie{ID: id, Expires: expires}
	var successor *int64
	err := s.cassandra.Query(`SELECT type,created,label,succ_id FROM user_cookies WHERE user=? AND expires=? AND id=?`, user, expires, int64(id)).
		WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).
		Scan(&cookie.Type, &cookie.Created, &cookie.Label, &successor)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, sessionError(err)
	}
	if successor != nil {
		x := uint32(*successor)
		cookie.Successor = &x
	}
	if cookie.Type != 0 && cookie.Type != 1 {
		return nil, errors.New("invalid stored cookie type")
	}
	return &cookie, nil
}

func (s sessionStore) list(ctx context.Context, user gocql.UUID) ([]sessionCookie, error) {
	iter := s.cassandra.Query(`SELECT id,created,expires,type,label,succ_id FROM user_cookies WHERE user=? ORDER BY expires ASC`, user).
		WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).Iter()
	// Empty JSON lists must be [] rather than null when exposed by the API.
	cookies := []sessionCookie{}
	var cookie sessionCookie
	var id int64
	var successor *int64
	for iter.Scan(&id, &cookie.Created, &cookie.Expires, &cookie.Type, &cookie.Label, &successor) {
		cookie.ID = uint32(id)
		cookie.Successor = nil
		if successor != nil {
			x := uint32(*successor)
			cookie.Successor = &x
		}
		if cookie.Type != 0 && cookie.Type != 1 {
			_ = iter.Close()
			return nil, errors.New("invalid stored cookie type")
		}
		cookies = append(cookies, cookie)
	}
	if err := iter.Close(); err != nil {
		return nil, sessionError(err)
	}
	return cookies, nil
}

func (s sessionStore) delete(ctx context.Context, user gocql.UUID, cookies []sessionCookie) error {
	if len(cookies) == 0 {
		return nil
	}
	batch := s.cassandra.NewBatch(gocql.UnloggedBatch).WithContext(ctx)
	batch.SetConsistency(gocql.LocalQuorum)
	batch.RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5})
	for _, cookie := range cookies {
		batch.Query(`DELETE FROM user_cookies WHERE user=? AND expires=? AND id=?`, user, cookie.Expires, int64(cookie.ID))
	}
	return sessionError(s.cassandra.ExecuteBatch(batch))
}

func (s sessionStore) deleteAll(ctx context.Context, user gocql.UUID) error {
	return sessionError(s.cassandra.Query(`DELETE FROM user_cookies WHERE user=?`, user).
		WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5}).Exec())
}

// Native eviction intentionally takes abs(freeSlots), at least one, rather
// than adding an extra eviction whenever a store is already over its limit.
func cookiesToEvict(limit int, now time.Time, cookies []sessionCookie) []sessionCookie {
	free := limit - len(cookies)
	if free > 0 {
		return nil
	}
	count := max(1, -free)
	ordered := append([]sessionCookie(nil), cookies...)
	sort.SliceStable(ordered, func(i, j int) bool {
		a, b := ordered[i], ordered[j]
		if a.Expires.Before(now) || b.Expires.Before(now) {
			// The store returns an expiration-ordered expired prefix. Preserve
			// that prefix as native list sorting does; Go sort requires a strict
			// comparator even when both entries have expired.
			return a.Expires.Before(now) && !b.Expires.Before(now)
		}
		if a.Successor != nil && b.Successor == nil {
			return true
		}
		if a.Successor == nil && b.Successor != nil {
			return false
		}
		return a.Created.Before(b.Created)
	})
	return ordered[:min(count, len(ordered))]
}
