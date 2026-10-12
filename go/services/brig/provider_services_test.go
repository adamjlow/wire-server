// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func TestServicePublicProjectionWithoutSecretColumns(t *testing.T) {
	name, description := "Synthetic", "Synthetic profile"
	enabled := false
	id, _ := gocql.ParseUUID("11111111-2222-3333-4444-555555555555")
	row := deletionServiceRow{name: &name, description: &description, enabled: &enabled, tags: []int64{4, 1, 4, 1 << 30}}
	fields, err := row.profile(id)
	if err != nil {
		t.Fatal(err)
	}
	if fields.Summary != "" || fields.Enabled || !reflect.DeepEqual(fields.Tags, []string{"audio", "business", "weather"}) || fields.Assets == nil {
		t.Fatal("native defaults/tag order changed")
	}
	encoded, _ := json.Marshal(providerServiceProfile{providerServiceFields: fields, Provider: id})
	var object map[string]json.RawMessage
	if json.Unmarshal(encoded, &object) != nil {
		t.Fatal("invalid profile JSON")
	}
	for _, secret := range []string{"auth_tokens", "public_keys", "base_url"} {
		if _, ok := object[secret]; ok {
			t.Fatal("public service leaked secret definition fields")
		}
	}
	if len(object) != 8 {
		t.Fatal("native public profile fields changed")
	}
	if _, err := row.definition(id); err == nil {
		t.Fatal("private service accepted missing required secret columns")
	}
}

func TestServiceDeleteOrderAndFailures(t *testing.T) {
	id, _ := gocql.ParseUUID("11111111-2222-3333-4444-555555555555")
	for _, tc := range []struct {
		name, fail     string
		wrong, missing bool
		steps          []string
	}{
		{"enqueue", "", false, false, []string{"password", "verify", "lookup", "disable", "enqueue"}},
		{"wrong password", "", true, false, []string{"password", "verify"}},
		{"missing service", "", false, true, []string{"password", "verify", "lookup"}},
		{"disable failure", "disable", false, false, []string{"password", "verify", "lookup", "disable"}},
		{"enqueue failure retains disabled state", "enqueue", false, false, []string{"password", "verify", "lookup", "disable", "enqueue"}},
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
			s := providerServiceHTTP{
				auth: providerAuthHTTP{password: func(context.Context, gocql.UUID) (*string, error) {
					step("password")
					v := "synthetic hash"
					return &v, nil
				}, verify: func(_ context.Context, key passwordRateKey, encoded, plain string) (bool, bool, error) {
					if key.kind != "provider" || key.id != id.String() {
						t.Fatal("wrong rate namespace")
					}
					return !tc.wrong, true, step("verify")
				}},
				lookup: func(context.Context, gocql.UUID, gocql.UUID) (*deletionServiceRecord, error) {
					step("lookup")
					if tc.missing {
						return nil, nil
					}
					return &deletionServiceRecord{}, nil
				},
				disable: func(context.Context, gocql.UUID, gocql.UUID) error { return step("disable") }, enqueue: func(context.Context, gocql.UUID, gocql.UUID) error { return step("enqueue") },
			}
			err := s.delete(context.Background(), id, id, "Goodp6")
			if !reflect.DeepEqual(steps, tc.steps) {
				t.Fatal("native delete ordering changed", steps)
			}
			if tc.wrong {
				if !errors.Is(err, authInvalidCredentials) {
					t.Fatal(err)
				}
			} else if tc.missing {
				if !errors.Is(err, errProviderServiceNotFound) {
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

func TestServiceHTTPGuardsAndPublicScope(t *testing.T) {
	id := "11111111-2222-3333-4444-555555555555"
	for _, public := range []bool{false, true} {
		for _, enabled := range []bool{false, true} {
			calls := 0
			s := providerServiceHTTP{auth: providerAuthHTTP{secondFactorEnabled: func(context.Context, string) (bool, error) { return enabled, nil }}, list: func(_ context.Context, provider gocql.UUID, actual bool) ([]any, error) {
				calls++
				if provider.String() != id || actual != public {
					t.Fatal("wrong scoped query")
				}
				return []any{}, nil
			}}
			path := "/provider/services"
			if public {
				path = "/providers/" + id + "/services"
			}
			r := httptest.NewRequest(http.MethodGet, path, nil)
			r.Header.Set("Z-Provider", id)
			r.Header.Set("Z-Type", "provider")
			r.Header.Set("Z-User", id)
			w := httptest.NewRecorder()
			s.handle(w, r, path)
			if enabled {
				if w.Code != 403 || calls != 0 {
					t.Fatal("feature guard bypass")
				}
			} else if w.Code != 200 || calls != 1 || strings.TrimSpace(w.Body.String()) != "[]" {
				t.Fatal("native empty list changed")
			}
		}
	}
	s := providerServiceHTTP{auth: providerAuthHTTP{secondFactorEnabled: func(context.Context, string) (bool, error) {
		t.Fatal("wrong token type passed guard")
		return false, nil
	}}}
	r := httptest.NewRequest(http.MethodGet, "/provider/services", nil)
	r.Header.Set("Z-Provider", id)
	r.Header.Set("Z-Type", "user")
	w := httptest.NewRecorder()
	s.handle(w, r, r.URL.Path)
	if w.Code != 403 || w.Body.Len() != 0 {
		t.Fatal("provider token guard changed")
	}
}
