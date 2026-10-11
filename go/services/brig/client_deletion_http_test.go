// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/apache/cassandra-gocql-driver/v2"
	"go.yaml.in/yaml/v3"
)

func TestClientDeletionPublicRouteAndAdmission(t *testing.T) {
	for _, tc := range []struct {
		name, body          string
		typ                 int32
		missing, connection bool
		status              int
	}{
		{"temporary", "{}", 0, false, true, 200},
		{"null password", "{\"password\":null}", 0, false, true, 200},
		{"first password wins", "{\"password\":\"secret6\",\"password\":\"short\"}", 0, false, true, 200},
		{"legalhold", "{}", 2, false, true, 400},
		{"missing", "{}", 0, true, true, 404},
		{"permanent missing auth", "{}", 1, false, true, 403},
		{"short password", "{\"password\":\"short\"}", 0, false, true, 400},
		{"missing connection", "{}", 0, false, false, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			removed := 0
			s := clientDeletionService{
				lookup: func(_ context.Context, _ gocql.UUID, id string) (*storedClient, error) {
					if id != "a" {
						t.Error("ID not normalized")
					}
					if tc.missing {
						return nil, nil
					}
					return &storedClient{ID: id, Type: tc.typ}, nil
				},
				reauthenticate: func(context.Context, gocql.UUID, *string) error { return authMissingPassword },
				enqueue:        func(context.Context, gocql.UUID, string, *string) error { return nil },
				remove:         func(context.Context, gocql.UUID, string) error { removed++; return nil },
			}
			h := newHandler(Config{}, &brigReadHandlers{clientDeletion: &s})
			r := httptest.NewRequest("DELETE", "/v19/clients/000A", strings.NewReader(tc.body))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Z-User", (gocql.UUID{}).String())
			if tc.connection {
				r.Header.Set("Z-Connection", "synthetic")
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if tc.status == 200 && (w.Body.Len() != 0 || w.Header().Get("Content-Type") != "" || removed != 1) {
				t.Fatal("native empty success response changed")
			}
		})
	}
	for _, text := range []string{"randomPrekeys: true", "randomPrekeys: false", "{}"} {
		var c Config
		if yaml.Unmarshal([]byte(text), &c) != nil {
			t.Fatal("configuration decode")
		}
		if text == "{}" && c.RandomPrekeys != nil || text != "{}" && (c.RandomPrekeys == nil || *c.RandomPrekeys != (text == "randomPrekeys: true")) {
			t.Fatal("native random-prekey switch lost")
		}
	}
}
