// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func TestProviderDeletionOrderAndFailureCutoffs(t *testing.T) {
	id, _ := gocql.ParseUUID("11111111-2222-3333-4444-555555555555")
	first, _ := gocql.ParseUUID("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	second, _ := gocql.ParseUUID("bbbbbbbb-cccc-dddd-eeee-ffffffffffff")
	all := []string{"account", "password", "verify", "list", "disconnect-first", "remove-first", "disconnect-second", "remove-second", "key", "provider"}
	for _, tc := range []struct {
		name           string
		missing, wrong bool
		fail           string
		want           int
	}{
		{"delete", false, false, "", 10}, {"missing/unactivated", true, false, "", 1}, {"wrong password", false, true, "", 3},
		{"list failure", false, false, "list", 4}, {"disconnect failure", false, false, "disconnect-first", 5},
		{"partial second service failure", false, false, "remove-second", 8}, {"key failure", false, false, "key", 9}, {"provider failure", false, false, "provider", 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var steps []string
			step := func(name string) error {
				steps = append(steps, name)
				if name == tc.fail {
					return context.Canceled
				}
				return nil
			}
			s := providerDeletionService{
				account: func(_ context.Context, actual gocql.UUID) (*providerAccount, error) {
					if actual != id {
						t.Fatal("wrong provider")
					}
					step("account")
					if tc.missing {
						return nil, nil
					}
					return &providerAccount{ID: id, Email: "synthetic@example.com"}, nil
				},
				auth: providerAuthHTTP{password: func(context.Context, gocql.UUID) (*string, error) {
					step("password")
					v := "synthetic hash"
					return &v, nil
				}, verify: func(_ context.Context, key passwordRateKey, encoded, plain string) (bool, bool, error) {
					if key.kind != "provider" || key.id != id.String() || plain != "Goodp6" {
						t.Fatal("wrong credential boundary")
					}
					return !tc.wrong, true, step("verify")
				}},
				services: func(context.Context, gocql.UUID) ([]providerDeletionServiceRecord, error) {
					return []providerDeletionServiceRecord{{id: first, record: deletionServiceRecord{name: "First", tags: []int64{1}}}, {id: second, record: deletionServiceRecord{name: "Second", tags: []int64{2}}}}, step("list")
				},
				disconnect: func(_ context.Context, actual, service gocql.UUID) error {
					if actual != id {
						t.Fatal("wrong provider")
					}
					if service == first {
						return step("disconnect-first")
					}
					if service != second {
						t.Fatal("wrong service")
					}
					return step("disconnect-second")
				},
				remove: func(_ context.Context, actual, service gocql.UUID, record deletionServiceRecord) error {
					if actual != id {
						t.Fatal("wrong provider")
					}
					if service == first {
						if record.name != "First" || !reflect.DeepEqual(record.tags, []int64{1}) {
							t.Fatal("wrong service record")
						}
						return step("remove-first")
					}
					if record.name != "Second" {
						t.Fatal("wrong second record")
					}
					return step("remove-second")
				},
				removeKey: func(_ context.Context, email string) error {
					if email != "synthetic@example.com" {
						t.Fatal("wrong email key")
					}
					return step("key")
				},
				removeAccount: func(_ context.Context, actual gocql.UUID) error {
					if actual != id {
						t.Fatal("wrong provider row")
					}
					return step("provider")
				},
			}
			err := s.execute(context.Background(), id, "Goodp6")
			if !reflect.DeepEqual(steps, all[:tc.want]) {
				t.Fatal("changed native failure/effect order", steps)
			}
			if tc.missing {
				if !errors.Is(err, errProviderDeletionInvalid) {
					t.Fatal(err)
				}
			} else if tc.wrong {
				if !errors.Is(err, authInvalidCredentials) {
					t.Fatal(err)
				}
			} else if tc.fail != "" {
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestProviderDeletionHTTPBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, typ, body string
		enabled         bool
		want            int
		calls           int
	}{
		{"feature guard", "provider", `{"password":"Goodp6"}`, true, 403, 1},
		{"wrong token type", "user", `{"password":"Goodp6"}`, false, 403, 0},
		{"missing password", "provider", `{}`, false, 400, 0},
		{"null password", "provider", `{"password":null}`, false, 400, 0},
		{"short password", "provider", `{"password":"short"}`, false, 400, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			s := providerDeletionService{auth: providerAuthHTTP{secondFactorEnabled: func(context.Context, string) (bool, error) { calls++; return tc.enabled, nil }}, account: func(context.Context, gocql.UUID) (*providerAccount, error) {
				t.Fatal("failed guard reached storage")
				return nil, nil
			}}
			r := httptest.NewRequest(http.MethodDelete, "/provider", strings.NewReader(tc.body))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Z-Type", tc.typ)
			r.Header.Set("Z-Provider", "11111111-2222-3333-4444-555555555555")
			w := httptest.NewRecorder()
			if !s.handle(w, r, r.URL.Path) || w.Code != tc.want || calls != tc.calls {
				t.Fatal("wrong deletion admission", w.Code, calls)
			}
		})
	}
}
