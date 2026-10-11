// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func TestClientDeletionNativeOrderAndPartialFailures(t *testing.T) {
	label := "synthetic-label"
	for _, tc := range []struct {
		name   string
		client *storedClient
		fail   string
		want   error
		events []string
	}{
		{"missing", nil, "", errClientMissing, []string{"lookup"}},
		{"legalhold", &storedClient{Type: 2}, "", errClientLegalHoldDeletion, []string{"lookup"}},
		{"permanent reauth", &storedClient{ID: "a", Type: 1, Cookie: &label}, "auth", authMissingPassword, []string{"lookup", "auth"}},
		{"permanent delete", &storedClient{ID: "a", Type: 1, Cookie: &label}, "", nil, []string{"lookup", "auth", "revoke", "enqueue", "remove"}},
		{"temporary skips auth", &storedClient{ID: "a", Type: 0}, "auth", nil, []string{"lookup", "enqueue", "remove"}},
		{"revoke failure", &storedClient{ID: "a", Type: 0, Cookie: &label}, "revoke", nil, []string{"lookup", "revoke"}},
		{"queue failure after revoke", &storedClient{ID: "a", Type: 0, Cookie: &label}, "enqueue", nil, []string{"lookup", "revoke", "enqueue"}},
		{"store failure after enqueue", &storedClient{ID: "a", Type: 0, Cookie: &label}, "remove", nil, []string{"lookup", "revoke", "enqueue", "remove"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			events := []string{}
			failure := errors.New("synthetic failure")
			step := func(name string) error {
				events = append(events, name)
				if tc.fail == name {
					if name == "auth" {
						return authMissingPassword
					}
					return failure
				}
				return nil
			}
			s := clientDeletionService{
				lookup: func(context.Context, gocql.UUID, string) (*storedClient, error) {
					events = append(events, "lookup")
					return tc.client, nil
				},
				reauthenticate: func(context.Context, gocql.UUID, *string) error { return step("auth") },
				revokeLabel: func(_ context.Context, _ gocql.UUID, value string) error {
					if value != label {
						t.Error("wrong cookie label")
					}
					return step("revoke")
				},
				enqueue: func(_ context.Context, _ gocql.UUID, id string, con *string) error {
					if id != "a" || con == nil || *con != "synthetic-connection" {
						t.Error("queue identity changed")
					}
					return step("enqueue")
				},
				remove: func(context.Context, gocql.UUID, string) error { return step("remove") },
			}
			err := s.removePublic(context.Background(), gocql.UUID{}, "a", "synthetic-connection", nil)
			want := tc.want
			if tc.fail != "" && tc.fail != "auth" {
				want = failure
			}
			if !errors.Is(err, want) || !reflect.DeepEqual(events, tc.events) {
				t.Fatalf("error=%v events=%v", err, events)
			}
		})
	}
}
