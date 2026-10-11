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
)

func TestFederationReadNativeConfigAndMerge(t *testing.T) {
	pointer := func(s string) *string { return &s }
	c := Config{}
	c.Settings.Other = map[string]any{"setFederationDomainConfigsUpdateFreq": 1}
	c.Settings.FederationDomains = &[]federationStaticDomain{
		{Domain: pointer("Z.Example.com"), SearchPolicy: pointer("full_search")},
		{Domain: pointer("a.example.com"), SearchPolicy: pointer("no_search")},
		{Domain: pointer("z.example.com"), SearchPolicy: pointer("full_search")},
	}
	s, err := federationReadFromConfig(c)
	if err != nil || s.strategy != "allowNone" || len(s.static) != 2 {
		t.Fatal("native fail-closed default/config map changed", s, err)
	}
	teamTag := int32(1)
	s.rows = func(context.Context) ([]federationRemoteRow, error) {
		return []federationRemoteRow{{domain: "z.example.com", searchPolicy: "no_search"}, {domain: "t.example.com", searchPolicy: "full_search", restriction: &teamTag}}, nil
	}
	s.teams = func(_ context.Context, domain string) ([]string, error) {
		if domain != "t.example.com" {
			t.Fatal("allow-all queried teams")
		}
		return nil, nil
	}
	result, err := s.read(context.Background())
	var domains []string
	for _, remote := range result.Remotes {
		domains = append(domains, remote.Domain)
	}
	if err != nil || result.UpdateInterval != 10 || !reflect.DeepEqual(domains, []string{"z.example.com", "t.example.com", "a.example.com", "z.example.com"}) {
		t.Fatal("native DB/file order, duplicates or fixed update interval changed", result, err)
	}
	encoded, _ := json.Marshal(result.Remotes[1].Restriction)
	if string(encoded) != `{"tag":"restrict_by_team","value":[]}` {
		t.Fatal("native empty-team encoding changed", string(encoded))
	}
	encoded, _ = json.Marshal(result.Remotes[0].Restriction)
	if string(encoded) != `{"tag":"allow_all","value":null}` {
		t.Fatal("native nullable restriction encoding changed", string(encoded))
	}
	(*c.Settings.FederationDomains)[2].SearchPolicy = pointer("exact_handle_search")
	if _, err := federationReadFromConfig(c); err == nil {
		t.Fatal("conflicting static domain policies accepted")
	}
	for _, strategy := range []string{"allowNone", "allowAll", "allowDynamic", "unknown"} {
		c.Settings.FederationDomains = nil
		c.Settings.FederationStrategy = &strategy
		_, err := federationReadFromConfig(c)
		if (err != nil) != (strategy == "unknown") {
			t.Fatal("native federation strategy validation changed", strategy, err)
		}
	}
}

func TestFederationReadStoredFailuresAndHTTP(t *testing.T) {
	domain := "A.Example.com"
	for _, policy := range []int32{0, 1, 2, 3} {
		got, name, err := federationSearchPolicy(&domain, &policy)
		if policy == 3 {
			if err == nil {
				t.Fatal("unknown search policy accepted")
			}
			continue
		}
		if err != nil || got != "a.example.com" || name != []string{"no_search", "exact_handle_search", "full_search"}[policy] {
			t.Fatal("native policy codec changed", got, name, err)
		}
	}
	policy := int32(2)
	if _, _, err := federationSearchPolicy(nil, &policy); err == nil {
		t.Fatal("null required domain accepted")
	}
	if _, _, err := federationSearchPolicy(&domain, nil); err == nil {
		t.Fatal("null required search policy accepted")
	}
	s, _ := federationReadFromConfig(Config{})
	tag := int32(2)
	s.rows = func(context.Context) ([]federationRemoteRow, error) {
		return []federationRemoteRow{{domain: domain, searchPolicy: "full_search", restriction: &tag}}, nil
	}
	if _, err := s.read(context.Background()); err == nil {
		t.Fatal("unknown restriction became allow-all")
	}
	tag = 1
	failure := errors.New("synthetic team store failure")
	s.teams = func(context.Context, string) ([]string, error) { return nil, failure }
	if _, err := s.read(context.Background()); !errors.Is(err, failure) {
		t.Fatal("team failure ignored", err)
	}
	s.rows = func(context.Context) ([]federationRemoteRow, error) { return nil, nil }
	handler := newHandler(Config{}, &brigReadHandlers{federationConfig: &s})
	for _, method := range []string{"GET", "HEAD"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(method, "/i/federation/remotes", nil))
		if w.Code != 200 || w.Header().Get("Content-Type") != "application/json;charset=utf-8" {
			t.Fatal("federation read HTTP contract changed")
		}
		if method == "HEAD" {
			if w.Body.Len() != 0 {
				t.Fatal("HEAD emitted federation body")
			}
			continue
		}
		var result federationDomainConfigs
		if json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Strategy != "allowNone" || result.UpdateInterval != 10 || result.Remotes == nil {
			t.Fatal("default federation JSON changed", w.Body.String())
		}
	}
}

func TestFederationVersionOriginAndUnitInput(t *testing.T) {
	for _, body := range []string{`{}`, `[]`, `null`, `42`, `"synthetic"`} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/federation/api-version", strings.NewReader(body))
		r.Header.Set("Wire-Origin-Domain", "Example.com")
		r.Header.Set("Content-Type", "application/json")
		newHandler(Config{}, &brigReadHandlers{}).ServeHTTP(w, r)
		var result struct {
			Supported []int `json:"supported"`
			Versions  []int `json:"supported_versions"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || !reflect.DeepEqual(result.Supported, []int{0, 1}) || !reflect.DeepEqual(result.Versions, []int{0, 1, 2, 3, 4}) {
			t.Fatal("native federation version negotiation changed", w.Code, w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/federation/api-version", strings.NewReader(`{}`))
	r.Header.Set("Content-Type", "application/json")
	federationVersionHTTP(w, r)
	if w.Code != 400 || w.Body.String() != "Header Wire-Origin-Domain is required" {
		t.Fatal("untrusted request without origin accepted")
	}
	for _, body := range []string{`{} {}`, `not JSON`} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/federation/api-version", strings.NewReader(body))
		r.Header.Set("Wire-Origin-Domain", "example.com")
		r.Header.Set("Content-Type", "application/json")
		federationVersionHTTP(w, r)
		if w.Code != 400 {
			t.Fatal("malformed federation body accepted")
		}
	}
}
