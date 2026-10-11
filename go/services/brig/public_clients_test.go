// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func TestPublicClientsRemoteOmissionAndLocalProjection(t *testing.T) {
	user := gocql.TimeUUID()
	trace := []string{}
	var traceMu sync.Mutex
	appendTrace := func(value string) { traceMu.Lock(); defer traceMu.Unlock(); trace = append(trace, value) }
	traceText := func() string { traceMu.Lock(); defer traceMu.Unlock(); return strings.Join(trace, ",") }
	peer := federationTestPeer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/api-version") {
			_, _ = io.WriteString(w, `{"supported":[0,1],"supported_versions":[4]}`)
			return
		}
		appendTrace(strings.Split(r.URL.Path, "/")[2])
		if strings.Contains(r.URL.Path, "/bad.example/") {
			w.WriteHeader(503)
			_, _ = io.WriteString(w, `{"code":503,"label":"synthetic","message":"synthetic"}`)
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/get-user-clients") {
			t.Error("wrong federation RPC")
		}
		var input struct {
			Users []string `json:"users"`
		}
		_ = json.NewDecoder(r.Body).Decode(&input)
		if len(input.Users) != 1 || input.Users[0] != user.String() {
			t.Error("wrong federation users")
		}
		_, _ = io.WriteString(w, `{"`+user.String()+`":[{"id":"000A","class":"desktop","label":"must disappear"},{"id":"a","class":"desktop"}]}`)
	})
	warnings := []string{}
	s := publicClientService{domain: "local.example", remote: &peer, warn: func(domain string) { warnings = append(warnings, domain) }, list: func(context.Context, gocql.UUID) ([]publicClient, error) {
		appendTrace("local")
		return []publicClient{{ID: "b"}}, nil
	}}
	result, err := s.qualified(context.Background(), map[string][]gocql.UUID{"good.example": {user}, "bad.example": {user}, "local.example": {user}}, "synthetic")
	if err != nil || len(result) != 2 || len(result["good.example"][user.String()]) != 1 || traceText() != "bad.example,good.example,local" || strings.Join(warnings, ",") != "bad.example" {
		t.Fatalf("result=%v trace=%v warnings=%v err=%v", result, traceText(), warnings, err)
	}
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), "label") || !strings.Contains(string(encoded), `"id":"a"`) {
		t.Fatalf("private metadata or alias leaked: %s", encoded)
	}
	result, err = s.qualified(context.Background(), map[string][]gocql.UUID{"bad.example": {user}}, "synthetic")
	if err != nil || len(result) != 1 || result["local.example"] == nil || len(result["local.example"]) != 0 {
		t.Fatalf("local empty domain omitted: %v %v", result, err)
	}
}

func TestPublicClientRouteVersionsAuthenticationAndProjection(t *testing.T) {
	user := gocql.TimeUUID()
	calls := 0
	class := "desktop"
	s := publicClientService{domain: "local.example", list: func(context.Context, gocql.UUID) ([]publicClient, error) {
		calls++
		return []publicClient{{ID: "a", Class: &class}}, nil
	}}
	for _, tc := range []struct {
		version, path, method, body string
		authenticated, handled      bool
		status                      int
	}{
		{"1", "/users/" + user.String() + "/clients", "GET", "", false, true, 200},
		{"2", "/users/" + user.String() + "/clients", "GET", "", false, false, 0},
		{"15", "/users/local.example/" + user.String() + "/clients", "GET", "", false, true, 200},
		{"16", "/users/local.example/" + user.String() + "/clients", "GET", "", false, false, 0},
		{"19", "/users/local.example/" + user.String() + "/clients/a", "GET", "", false, true, 200},
		{"19", "/users/local.example/" + user.String() + "/clients/f", "GET", "", false, true, 404},
		{"1", "/users/list-clients", "POST", `[{"domain":"local.example","id":"` + user.String() + `"}]`, true, true, 200},
		{"19", "/users/list-clients", "POST", `{"qualified_users":[{"domain":"local.example","id":"` + user.String() + `"}]}`, true, true, 200},
		{"19", "/users/list-clients", "POST", `{"qualified_users":[]}`, true, true, 400},
		{"19", "/users/list-clients", "POST", `{}`, false, true, 400},
		{"19", "/users/list-clients/v2", "POST", `{}`, true, false, 0},
	} {
		r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		r.Header.Set("X-Wire-API-Version", tc.version)
		r.Header.Set("Content-Type", "application/json")
		if tc.authenticated {
			r.Header.Set("Z-User", user.String())
		}
		w := httptest.NewRecorder()
		before := calls
		handled := s.handle(w, r, tc.path)
		if handled != tc.handled || handled && w.Code != tc.status {
			t.Fatalf("v%s %s handled=%v code=%d body=%s", tc.version, tc.path, handled, w.Code, w.Body.String())
		}
		if (!handled || tc.status == 400) && calls != before {
			t.Fatal("invalid request accessed storage")
		}
		if handled && tc.status == 200 {
			if strings.Contains(w.Body.String(), "capabilities") || strings.Contains(w.Body.String(), "mls_public_keys") {
				t.Fatal("private metadata exposed")
			}
		}
	}
}
