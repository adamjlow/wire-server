// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func TestAccountLookupNativeArmsFiltersAndOrdering(t *testing.T) {
	var ids [5]gocql.UUID
	for i := range ids {
		ids[i][15] = byte(i + 1)
	}
	email := "synthetic@example.com"
	pending, app, deleted := int32(4), int32(2), int32(2)
	rows := map[gocql.UUID]storedUser{
		ids[0]: {ID: ids[0], Name: "Activated", Email: &email, Activated: true},
		ids[1]: {ID: ids[1], Name: "Expired", Email: &email, Activated: true, Status: &pending},
		ids[2]: {ID: ids[2], Name: "App", Type: &app},
		ids[3]: {ID: ids[3], Name: "No identity"},
		ids[4]: {ID: ids[4], Name: "Deleted", Email: &email, Activated: true, Status: &deleted},
	}
	steps := []string{}
	s := accountLookup{domain: "example.com", locale: "en", resolve: func(_ context.Context, key loginIdentifier) (*gocql.UUID, error) {
		if key.email != nil {
			steps = append(steps, "email")
			return &ids[1], nil
		}
		steps = append(steps, "handle")
		return &ids[0], nil
	}, users: func(_ context.Context, requested []gocql.UUID) ([]storedUser, error) {
		steps = append(steps, "users")
		result := []storedUser{}
		for _, id := range requested {
			if row, ok := rows[id]; ok {
				result = append(result, row)
			}
		}
		return result, nil
	}, invitations: func(context.Context, string) ([]storedInvitation, error) {
		steps = append(steps, "invitation")
		return nil, nil
	}, enqueueDeletion: func(_ context.Context, id gocql.UUID) error {
		steps = append(steps, "enqueue")
		if id != ids[1] {
			t.Fatal("wrong deleted account")
		}
		return nil
	}}
	result, err := s.lookup(context.Background(), accountLookupRequest{IDs: []gocql.UUID{ids[0], ids[1], ids[2], ids[3], ids[4]}, Handles: []string{"synthetic"}, Emails: []string{email, email}})
	if err != nil || len(result) != 3 || result[0].ID != ids[0].String() || result[1].ID != ids[2].String() || result[2].ID != ids[1].String() {
		t.Fatal("native branch/filter/duplicate behavior", result, err)
	}
	if !reflect.DeepEqual(steps, []string{"email", "users", "email", "users", "handle", "users", "invitation", "enqueue"}) {
		t.Fatal("native lookup/effect ordering changed", steps)
	}
	// Identical email-arm profiles are deduplicated, but cross-arm copies survive.
	s.resolve = func(context.Context, loginIdentifier) (*gocql.UUID, error) { return &ids[0], nil }
	result, err = s.lookup(context.Background(), accountLookupRequest{IDs: []gocql.UUID{ids[0]}, Emails: []string{email, email}})
	if err != nil || len(result) != 2 || result[0].ID != result[1].ID {
		t.Fatal("cross-arm duplicate removed", err)
	}
}

func TestAccountLookupPartitionsBeforeQueueAndPreservesPendingControl(t *testing.T) {
	var first, second gocql.UUID
	first[15] = 1
	second[15] = 2
	one, two := "one@example.com", "two@example.com"
	pending := int32(4)
	s := accountLookup{domain: "example.com", resolve: func(context.Context, loginIdentifier) (*gocql.UUID, error) { return nil, nil }, users: func(context.Context, []gocql.UUID) ([]storedUser, error) {
		return []storedUser{{ID: first, Name: "First", Activated: true, Email: &one, Status: &pending}, {ID: second, Name: "Second", Activated: true, Email: &two, Status: &pending}}, nil
	}}
	queued := 0
	looked := 0
	s.enqueueDeletion = func(context.Context, gocql.UUID) error { queued++; return nil }
	s.invitations = func(_ context.Context, email string) ([]storedInvitation, error) {
		looked++
		if email == two {
			return nil, errors.New("synthetic lookup failure")
		}
		return nil, nil
	}
	if _, err := s.lookup(context.Background(), accountLookupRequest{IDs: []gocql.UUID{first, second}}); err == nil || queued != 0 || looked != 2 {
		t.Fatal("queue write occurred before complete invitation partition")
	}
	s.invitations = func(context.Context, string) ([]storedInvitation, error) { return []storedInvitation{{}}, nil }
	for _, include := range []bool{false, true} {
		result, err := s.lookup(context.Background(), accountLookupRequest{IDs: []gocql.UUID{first, second}, IncludePending: include})
		want := 0
		if include {
			want = 2
		}
		if err != nil || len(result) != want || queued != 0 {
			t.Fatal("pending control changed", err)
		}
	}
	s.invitations = func(context.Context, string) ([]storedInvitation, error) { return nil, nil }
	s.enqueueDeletion = func(context.Context, gocql.UUID) error { return errors.New("synthetic enqueue failure") }
	if result, err := s.lookup(context.Background(), accountLookupRequest{IDs: []gocql.UUID{first}}); err == nil || result != nil {
		t.Fatal("queue failure masked as successful empty lookup")
	}
}

func TestAccountLookupQueryValidationBeforeDependencies(t *testing.T) {
	calls := 0
	s := accountLookup{users: func(context.Context, []gocql.UUID) ([]storedUser, error) { calls++; return nil, nil }, resolve: func(context.Context, loginIdentifier) (*gocql.UUID, error) { calls++; return nil, nil }, invitations: func(context.Context, string) ([]storedInvitation, error) { calls++; return nil, nil }, enqueueDeletion: func(context.Context, gocql.UUID) error { calls++; return nil }}
	handler := newHandler(Config{}, &brigReadHandlers{accounts: &s})
	for _, query := range []string{"ids=bad", "handles=BAD", "email=bad", "ids=00000000-0000-0000-0000-000000000001&includePendingInvitations=invalid"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest("GET", "/i/users?"+query, nil))
		if recorder.Code != 400 || calls != 0 {
			t.Fatal("invalid query reached storage", query)
		}
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest("GET", "/i/users", nil))
	if recorder.Code != 404 || !strings.Contains(recorder.Body.String(), "no user keys") || calls != 0 {
		t.Fatal("missing native keys error", recorder)
	}
	for _, method := range []string{"GET", "HEAD"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(method, "/i/users?ids=00000000-0000-0000-0000-000000000001", nil))
		if recorder.Code != 200 || recorder.Header().Get("Content-Type") != "application/json;charset=utf-8" || method == "GET" && strings.TrimSpace(recorder.Body.String()) != "[]" || method == "HEAD" && recorder.Body.Len() != 0 {
			t.Fatal("native empty/HEAD response", recorder)
		}
	}
}
