// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func TestRemoteDeletionAdmissionBeforeEffects(t *testing.T) {
	user := gocql.TimeUUID().String()
	for _, scenario := range []struct {
		method, origin, body string
		status               int
	}{
		{"GET", "remote.example.com", `{}`, 405},
		{"POST", "", `{}`, 400},
		{"POST", "remote.example.com", `{"user":"` + user + `","connections":[]}`, 400},
		{"POST", "remote.example.com", `{"User":"` + user + `","connections":["` + user + `"]}`, 400},
		{"POST", "remote.example.com", `{"user":"` + user + `","connections":["invalid"]}`, 400},
	} {
		calls := 0
		s := remoteAccountDeletion{statuses: func(context.Context, string, gocql.UUID, []gocql.UUID) ([]gocql.UUID, error) {
			calls++
			return nil, nil
		}}
		r := httptest.NewRequest(scenario.method, "/federation/on-user-deleted-connections", strings.NewReader(scenario.body))
		r.Header.Set("Wire-Origin-Domain", scenario.origin)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.handle(w, r, r.URL.Path)
		if w.Code != scenario.status || calls != 0 {
			t.Fatalf("status=%d effects=%d", w.Code, calls)
		}
	}
	values := make([]string, 1001)
	for i := range values {
		values[i] = user
	}
	raw, _ := json.Marshal(map[string]any{"user": user, "connections": values})
	if _, _, err := decodeRemoteAccountDeletion(raw); err == nil {
		t.Fatal("unbounded recipient list accepted")
	}
}

func TestRemoteDeletionReadsPushesBeforeDeletes(t *testing.T) {
	for _, failure := range []string{"", "lookup", "delete"} {
		t.Run(failure, func(t *testing.T) {
			deleted, accepted, other := gocql.TimeUUID(), gocql.TimeUUID(), gocql.TimeUUID()
			var calls []string
			s := remoteAccountDeletion{
				statuses: func(_ context.Context, domain string, u gocql.UUID, locals []gocql.UUID) ([]gocql.UUID, error) {
					calls = append(calls, "lookup")
					if domain != "remote.example.com" || u != deleted || !reflect.DeepEqual(locals, []gocql.UUID{accepted, other}) {
						t.Error("incorrect lookup")
					}
					if failure == "lookup" {
						return nil, errors.New("synthetic")
					}
					return []gocql.UUID{accepted}, nil
				},
				notify: func(_ context.Context, raw []byte, rid string) {
					calls = append(calls, "push")
					var pushes []struct {
						Origin     string
						Recipients []struct {
							User  string `json:"user_id"`
							Route string
						}
						Payload []struct {
							Type, ID  string
							Qualified struct{ ID, Domain string } `json:"qualified_id"`
						}
					}
					if json.Unmarshal(raw, &pushes) != nil || len(pushes) != 1 {
						t.Fatal("invalid push")
					}
					p := pushes[0]
					if rid != "synthetic-delete" || p.Origin != deleted.String() || len(p.Recipients) != 1 || p.Recipients[0].User != accepted.String() || p.Recipients[0].Route != "direct" || len(p.Payload) != 1 || p.Payload[0].Type != "user.delete" || p.Payload[0].Qualified.Domain != "remote.example.com" || p.Payload[0].Qualified.ID != deleted.String() {
						t.Error("incorrect remote deletion event")
					}
				},
				remove: func(_ context.Context, domain string, u gocql.UUID, locals []gocql.UUID) error {
					calls = append(calls, "delete")
					if len(locals) != 2 {
						t.Error("nonaccepted contacts not removed")
					}
					if failure == "delete" {
						return errors.New("synthetic")
					}
					return nil
				},
			}
			raw, _ := json.Marshal(map[string]any{"user": deleted.String(), "connections": []string{accepted.String(), other.String()}})
			r := httptest.NewRequest("POST", "/federation/on-user-deleted-connections", strings.NewReader(string(raw)))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Wire-Origin-Domain", "remote.example.com")
			r.Header.Set("Request-Id", "synthetic-delete")
			w := httptest.NewRecorder()
			s.handle(w, r, r.URL.Path)
			want := []string{"lookup", "push", "delete"}
			status := 200
			if failure == "lookup" {
				want = want[:1]
			}
			if failure != "" {
				status = 500
			}
			if w.Code != status || !reflect.DeepEqual(calls, want) {
				t.Fatalf("status=%d calls=%v", w.Code, calls)
			}
			if failure == "" && (w.Header().Get("Content-Type") != "application/json;charset=utf-8" || strings.TrimSpace(w.Body.String()) != "{}") {
				t.Fatal("native EmptyResponse changed")
			}
		})
	}
}
