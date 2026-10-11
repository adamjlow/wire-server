// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type lifecycleTestStore struct {
	memoryCookieStore
	existsValue bool
	steps       []string
	fail        string
	status      int32
}

func (s *lifecycleTestStore) step(name string) error {
	s.steps = append(s.steps, name)
	if s.fail == name {
		return errors.New("synthetic lifecycle failure")
	}
	return nil
}
func (s *lifecycleTestStore) exists(context.Context, gocql.UUID) (bool, error) {
	return s.existsValue, s.step("exists")
}
func (s *lifecycleTestStore) updateAccountStatus(_ context.Context, _ gocql.UUID, status int32) error {
	if err := s.step("status"); err != nil {
		return err
	}
	s.status = status
	return nil
}
func (s *lifecycleTestStore) list(ctx context.Context, user gocql.UUID) ([]sessionCookie, error) {
	if err := s.step("list"); err != nil {
		return nil, err
	}
	return s.memoryCookieStore.list(ctx, user)
}
func (s *lifecycleTestStore) deleteAll(ctx context.Context, user gocql.UUID) error {
	if err := s.step("revoke"); err != nil {
		return err
	}
	return s.memoryCookieStore.deleteAll(ctx, user)
}

func TestInactivityCreationThresholdAndDisabledPolicy(t *testing.T) {
	ctx, now, user := context.Background(), time.Unix(1000, 0), gocql.UUID{}
	timeout := 100 * time.Second
	for _, tc := range []struct {
		name     string
		timeout  *time.Duration
		cookies  []sessionCookie
		inactive bool
	}{
		{"disabled", nil, []sessionCookie{{Created: now.Add(-101 * time.Second)}}, false},
		{"empty", &timeout, nil, false},
		{"equal", &timeout, []sessionCookie{{Created: now.Add(-timeout)}}, false},
		{"young session", &timeout, []sessionCookie{{Type: 1, Created: now.Add(-101 * time.Second)}, {Type: 0, Created: now}}, false},
		{"young persistent", &timeout, []sessionCookie{{Type: 0, Created: now.Add(-101 * time.Second)}, {Type: 1, Created: now}}, false},
		{"expired but young", &timeout, []sessionCookie{{Created: now, Expires: now.Add(-time.Second)}}, false},
		{"old but unexpired", &timeout, []sessionCookie{{Created: now.Add(-101 * time.Second), Expires: now.Add(time.Hour)}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &lifecycleTestStore{existsValue: true, memoryCookieStore: memoryCookieStore{cookies: tc.cookies}}
			a := accountLifecycle{accounts: s, cookies: s, syncIndex: func(context.Context, gocql.UUID) error { return s.step("index") }}
			err := a.suspendInactive(ctx, user, tc.timeout, now)
			if tc.inactive {
				if !errors.Is(err, ErrExpiredToken) || s.status != 1 || len(s.cookies) != 0 || !slices.Equal(s.steps, []string{"list", "exists", "revoke", "status", "index"}) {
					t.Fatal("inactive account did not revoke, suspend and index before expiry", err, s.steps)
				}
			} else if err != nil || s.status != 0 || len(s.steps) > 1 {
				t.Fatal("active account mutated", err, s.steps)
			}
		})
	}
}

func TestAccountStatusFailureOrderingAndMissingAccount(t *testing.T) {
	ctx, user := context.Background(), gocql.UUID{}
	for _, fail := range []string{"exists", "revoke", "status", "index"} {
		t.Run(fail, func(t *testing.T) {
			s := &lifecycleTestStore{existsValue: true, fail: fail}
			a := accountLifecycle{accounts: s, cookies: s, syncIndex: func(context.Context, gocql.UUID) error { return s.step("index") }}
			if err := a.changeStatus(ctx, user, 1); err == nil {
				t.Fatal("failed dependency swallowed")
			}
			all := []string{"exists", "revoke", "status", "index"}
			if !slices.Equal(s.steps, all[:slices.Index(all, fail)+1]) {
				t.Fatal("continued after failure", s.steps)
			}
		})
	}
	s := &lifecycleTestStore{existsValue: false, memoryCookieStore: memoryCookieStore{cookies: []sessionCookie{{Created: time.Unix(1, 0)}}}}
	a := accountLifecycle{accounts: s, cookies: s}
	timeout := time.Second
	if err := a.suspendInactive(ctx, user, &timeout, time.Unix(100, 0)); !errors.Is(err, ErrExpiredToken) || !slices.Equal(s.steps, []string{"list", "exists"}) || len(s.cookies) != 1 {
		t.Fatal("missing inactive account must expire without creating a row", err, s.steps)
	}
	s.existsValue, s.steps = true, nil
	if err := a.changeStatus(ctx, user, 1); err == nil || !slices.Equal(s.steps, []string{"exists"}) {
		t.Fatal("unwired index allowed mutation")
	}
	a.syncIndex = func(context.Context, gocql.UUID) error { return s.step("index") }
	s.steps = nil
	if err := a.changeStatus(ctx, user, 0); err != nil || !slices.Equal(s.steps, []string{"exists", "status", "index"}) {
		t.Fatal("resume must not revoke", err, s.steps)
	}
	s.steps = nil
	if err := a.changeStatus(ctx, user, 2); !errors.Is(err, errInvalidAccountStatus) || !slices.Equal(s.steps, []string{"exists"}) {
		t.Fatal("deleted is not a status transition")
	}
}
