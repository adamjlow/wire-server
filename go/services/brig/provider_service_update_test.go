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

func TestServiceUpdateNativeAdmission(t *testing.T) {
	for _, body := range []string{`{}`, `{"name":null,"summary":null,"description":null,"assets":null,"tags":null}`, `{"name":"Synthetic","assets":[],"tags":["audio","audio"]}`} {
		if _, err := decodeProviderServiceUpdate(json.RawMessage(body)); err != nil {
			t.Fatal(err)
		}
	}
	for _, body := range []string{`null`, `{"name":""}`, `{"summary":"` + strings.Repeat("界", 129) + `"}`, `{"description":""}`, `{"assets":false}`, `{"tags":[]}`, `{"tags":["unknown"]}`} {
		if _, err := decodeProviderServiceUpdate(json.RawMessage(body)); err == nil {
			t.Fatal("invalid update accepted")
		}
	}
}

func TestServiceUpdatePreservesNativeIndexOperations(t *testing.T) {
	id, _ := gocql.ParseUUID("11111111-2222-3333-4444-555555555555")
	old := providerServiceDefinition{providerServiceFields: providerServiceFields{Name: "Alpha", Tags: []string{"audio", "books"}, Enabled: true}}
	name := "Beta"
	tags := []int64{4}
	queries, err := serviceUpdateQueries(id, id, old, providerServiceUpdate{name: &name, tags: &tags})
	if err != nil {
		t.Fatal(err)
	}
	if len(queries) != 14 || !strings.Contains(queries[0].statement, "SET name") || !strings.Contains(queries[1].statement, "DELETE FROM service_prefix") || !strings.Contains(queries[2].statement, "INSERT INTO service_prefix") || !strings.Contains(queries[9].statement, "SET tags") {
		t.Fatal("native name/tag batch stages changed", len(queries))
	}
	inserted := []int64{}
	for _, query := range queries {
		if strings.HasPrefix(query.statement, "INSERT INTO service_tag") {
			if query.values[2] != "beta" {
				t.Fatal("wrong lowercase name")
			}
			inserted = append(inserted, query.values[1].(int64))
		}
	}
	if !reflect.DeepEqual(inserted, []int64{1, 2, 3, 4}) {
		t.Fatal("native combined update intermediate tag writes removed", inserted)
	}
	old.Enabled = false
	queries, err = serviceUpdateQueries(id, id, old, providerServiceUpdate{name: &name, tags: &tags})
	if err != nil || len(queries) != 2 {
		t.Fatal("disabled service wrote indexes")
	}
	old.Enabled = true
	queries, err = serviceUpdateQueries(id, id, old, providerServiceUpdate{tags: &tags})
	if err != nil || len(queries) != 5 {
		t.Fatal("same-name mask differences changed")
	}
}

func TestServiceUpdateAccountAndServiceBeforeWrite(t *testing.T) {
	id, _ := gocql.ParseUUID("11111111-2222-3333-4444-555555555555")
	for _, missing := range []string{"account", "service", ""} {
		var steps []string
		s := providerServiceProfileUpdate{account: func(context.Context, gocql.UUID) (*providerAccount, error) {
			steps = append(steps, "account")
			if missing == "account" {
				return nil, nil
			}
			return &providerAccount{}, nil
		}, get: func(_ context.Context, p, s gocql.UUID, public bool) (any, error) {
			steps = append(steps, "service")
			if public {
				t.Fatal("public projection used to mutate")
			}
			if missing == "service" {
				return nil, nil
			}
			return providerServiceDefinition{}, nil
		}, write: func(context.Context, gocql.UUID, gocql.UUID, providerServiceDefinition, providerServiceUpdate) error {
			steps = append(steps, "write")
			return nil
		}}
		err := s.update(context.Background(), id, id, providerServiceUpdate{})
		if missing == "account" {
			if !errors.Is(err, errProviderDeletionInvalid) || len(steps) != 1 {
				t.Fatal(err, steps)
			}
		} else if missing == "service" {
			if !errors.Is(err, errProviderServiceNotFound) || len(steps) != 2 {
				t.Fatal(err, steps)
			}
		} else if err != nil || !reflect.DeepEqual(steps, []string{"account", "service", "write"}) {
			t.Fatal(err, steps)
		}
	}
}

func TestServiceUpdateAcceptAndFeatureGuard(t *testing.T) {
	for _, accept := range []string{"", "application/json", "text/plain", "*/*", "text/html, application/json;q=0.8"} {
		r := httptest.NewRequest(http.MethodPut, "/provider/services", nil)
		r.Header.Set("Accept", accept)
		if !serviceUpdateAccept(r) {
			t.Fatal("native supported Accept rejected")
		}
	}
	r := httptest.NewRequest(http.MethodPut, "/provider/services", strings.NewReader(`{}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/xml")
	s := providerServiceProfileUpdate{auth: providerAuthHTTP{secondFactorEnabled: func(context.Context, string) (bool, error) {
		t.Fatal("unsupported Accept reached guard")
		return false, nil
	}}}
	w := httptest.NewRecorder()
	s.handle(w, r, gocql.UUID{}, gocql.UUID{})
	if w.Code != 406 {
		t.Fatal("unsupported Accept admitted")
	}
}
