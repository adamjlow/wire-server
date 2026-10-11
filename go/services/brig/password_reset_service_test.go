// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type memoryResetCodes struct {
	rows       map[string]passwordResetRecord
	ttl        int32
	deleted    int
	failDelete error
}

func (s *memoryResetCodes) lookup(_ context.Context, key string) (*passwordResetRecord, error) {
	row, ok := s.rows[key]
	if !ok {
		return nil, nil
	}
	return &row, nil
}
func (s *memoryResetCodes) insert(_ context.Context, key string, row passwordResetRecord, ttl int32) error {
	s.rows[key] = row
	s.ttl = ttl
	return nil
}
func (s *memoryResetCodes) delete(_ context.Context, key string) error {
	if s.failDelete != nil {
		return s.failDelete
	}
	delete(s.rows, key)
	s.deleted++
	return nil
}

func resetFixture(t *testing.T) (passwordResetService, *memoryResetCodes, *storedUser) {
	t.Helper()
	user := storedUser{ID: gocql.UUID{1}, Name: "Synthetic", Activated: true}
	store := &memoryResetCodes{rows: map[string]passwordResetRecord{}}
	service := passwordResetService{codes: store, activeEmail: func(context.Context, string) (*storedUser, error) { return &user, nil }, account: func(context.Context, gocql.UUID) (*storedUser, error) { return &user, nil }, currentPassword: func(context.Context, gocql.UUID) (*string, error) { return nil, nil }, verifyPassword: func(context.Context, passwordRateKey, string, string) (bool, bool, error) { return false, false, nil }, hashPassword: func(_ context.Context, key passwordRateKey, plain string, min int) (string, error) {
		if key.kind != "user" || key.id != user.ID.String() || min != 8 {
			t.Fatal("native reset hash key changed")
		}
		return "synthetic-hash", nil
	}, setPassword: func(context.Context, gocql.UUID, string) error { return nil }, deleteCookies: func(context.Context, gocql.UUID) error { return nil }, mail: func(context.Context, string, string, string, string) error { return nil }, newCode: func() (string, error) { return "synthetic-code", nil }, now: func() time.Time { return time.Unix(100, 0).UTC() }, domain: "example.com", locale: "en"}
	return service, store, &user
}

func TestPasswordResetPrivacyAndMailFailurePersistence(t *testing.T) {
	ctx := context.Background()
	email := "synthetic@example.com"
	for _, mode := range []string{"missing", "disallowed", "saml", "pending"} {
		t.Run(mode, func(t *testing.T) {
			s, store, user := resetFixture(t)
			s.mail = func(context.Context, string, string, string, string) error {
				t.Fatal("private admission sent mail")
				return nil
			}
			switch mode {
			case "missing":
				s.activeEmail = func(context.Context, string) (*storedUser, error) { return nil, nil }
			case "disallowed":
				domains := []string{"other.example.com"}
				s.allowDomains = &domains
				s.activeEmail = func(context.Context, string) (*storedUser, error) {
					t.Fatal("disallowed email reached accounts")
					return nil, nil
				}
			case "saml":
				user.SSO = []byte(`{"tenant":"https://synthetic.example.com","subject":"synthetic"}`)
			case "pending":
				until := s.now().Add(time.Hour)
				attempts := int32(3)
				store.rows[passwordResetKey(user.ID)] = passwordResetRecord{user: user.ID, code: "existing-code", timeout: &until, retries: &attempts}
			}
			if err := s.begin(ctx, passwordResetRequest{email: &email}); err != nil {
				t.Fatal("private admission escaped", err)
			}
			if store.ttl != 0 {
				t.Fatal("private admission rewrote reset")
			}
		})
	}
	s, store, user := resetFixture(t)
	failure := errors.New("synthetic delivery failure")
	s.mail = func(_ context.Context, recipient, key, code, locale string) error {
		if recipient != email || key != passwordResetKey(user.ID) || code != "synthetic-code" || locale != "en" {
			t.Fatal("native mail inputs changed")
		}
		return failure
	}
	if err := s.begin(ctx, passwordResetRequest{email: &email}); !errors.Is(err, failure) || store.ttl != 3600 {
		t.Fatal("mail failure lost persisted reset", err)
	}
	pair, err := s.pair(ctx, email)
	if err != nil || pair["code"] != "synthetic-code" {
		t.Fatal("internal pair after mail failure changed")
	}
}

func TestPasswordResetAttemptsAndExpiryBoundaries(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		name    string
		offset  time.Duration
		correct bool
		retries *int32
		deleted bool
	}{
		{"correct-at-expiry", 0, true, nil, true},
		{"incorrect-at-expiry", 0, false, ptrResetAttempts(3), true},
		{"incorrect-before-expiry", time.Second, false, ptrResetAttempts(3), false},
		{"last-attempt", time.Second, false, ptrResetAttempts(1), true},
		{"missing-attempts", time.Second, false, nil, true},
		{"correct-expired", -time.Nanosecond, true, ptrResetAttempts(3), true},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, store, user := resetFixture(t)
			key := passwordResetKey(user.ID)
			until := s.now().Add(c.offset)
			store.rows[key] = passwordResetRecord{user: user.ID, code: "correct-code", retries: c.retries, timeout: &until}
			code := "wrong-code"
			if c.correct {
				code = "correct-code"
			}
			err := s.complete(ctx, passwordResetRequest{key: &key, code: code, password: "new-password"})
			if c.correct && c.offset >= 0 {
				if err != nil {
					t.Fatal("inclusive native expiry rejected", err)
				}
			} else if !errors.Is(err, errPasswordResetCode) {
				t.Fatal("wrong code result", err)
			}
			if (store.deleted == 1) != c.deleted {
				t.Fatal("retry deletion changed")
			}
			if !c.deleted {
				row := store.rows[key]
				if row.retries == nil || *row.retries != 2 || row.timeout != &until || store.ttl != 3600 {
					t.Fatal("whole-row retry/logical expiry changed")
				}
			}
		})
	}
}
func ptrResetAttempts(value int32) *int32 { return &value }

func TestPasswordResetMutationOrderAndSecurityFailures(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []string{"success", "same", "saml", "rate", "write", "delete"} {
		t.Run(mode, func(t *testing.T) {
			s, store, user := resetFixture(t)
			key := passwordResetKey(user.ID)
			until := s.now().Add(time.Hour)
			attempts := int32(3)
			store.rows[key] = passwordResetRecord{user: user.ID, code: "correct-code", timeout: &until, retries: &attempts}
			current := "old-hash"
			s.currentPassword = func(context.Context, gocql.UUID) (*string, error) { return &current, nil }
			var events []string
			failure := errors.New("synthetic storage failure")
			s.verifyPassword = func(context.Context, passwordRateKey, string, string) (bool, bool, error) {
				events = append(events, "verify")
				return mode == "same", true, nil
			}
			s.hashPassword = func(context.Context, passwordRateKey, string, int) (string, error) {
				events = append(events, "hash")
				if mode == "rate" {
					return "", passwordRateExceeded{micros: 1000000}
				}
				return "new-hash", nil
			}
			s.setPassword = func(context.Context, gocql.UUID, string) error {
				events = append(events, "write")
				if mode == "write" {
					return failure
				}
				return nil
			}
			s.deleteCookies = func(context.Context, gocql.UUID) error {
				events = append(events, "cookies")
				if _, present := store.rows[key]; present {
					t.Fatal("cookies revoked before reset deletion")
				}
				return nil
			}
			if mode == "saml" {
				user.SSO = []byte(`{"tenant":"https://synthetic.example.com","subject":"synthetic"}`)
			}
			if mode == "delete" {
				store.failDelete = failure
			}
			err := s.complete(ctx, passwordResetRequest{key: &key, code: "correct-code", password: "new-password"})
			expected := []string{"verify", "hash", "write", "cookies"}
			switch mode {
			case "success":
				if err != nil {
					t.Fatal(err)
				}
			case "same":
				expected = []string{"verify"}
				if !errors.Is(err, errPasswordResetSame) {
					t.Fatal("same password changed")
				}
			case "saml":
				expected = nil
				if !errors.Is(err, errPasswordResetCode) {
					t.Fatal("SAML reset accepted")
				}
			case "rate":
				expected = []string{"verify", "hash"}
				var limit passwordRateExceeded
				if !errors.As(err, &limit) {
					t.Fatal("rate failure changed")
				}
			case "write", "delete":
				expected = []string{"verify", "hash", "write"}
				if !errors.Is(err, failure) {
					t.Fatal("storage failure hidden")
				}
			}
			if !reflect.DeepEqual(events, expected) {
				t.Fatal("mutation/partial-failure order changed", events)
			}
			if mode != "success" && store.deleted != 0 {
				t.Fatal("failed reset deleted code")
			}
		})
	}
}
