// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func TestFederationRemotePoolJoinsFailuresAfterLocalEffects(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	first, rest := make(chan struct{}), make(chan struct{})
	var releaseFirst, releaseRest sync.Once
	defer releaseFirst.Do(func() { close(first) })
	defer releaseRest.Do(func() { close(rest) })
	arrived := make(chan string, 9)
	var active, peak, completed, localClaims atomic.Int32
	client := federationTestPeer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/api-version") {
			if localClaims.Load() != 1 {
				t.Error("remote call preceded local consumption")
			}
			_, _ = io.WriteString(w, `{"supported":[0,1],"supported_versions":[4]}`)
			return
		}
		n := active.Add(1)
		for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
		}
		defer active.Add(-1)
		domain := strings.Split(r.URL.Path, "/")[2]
		arrived <- domain
		gate := rest
		if domain == "d0.example" {
			gate = first
		}
		select {
		case <-gate:
		case <-r.Context().Done():
			return
		}
		completed.Add(1)
		if domain == "d0.example" {
			w.WriteHeader(503)
			_, _ = io.WriteString(w, `{"code":503,"label":"synthetic-down","message":"synthetic"}`)
		} else {
			_, _ = io.WriteString(w, `{}`)
		}
	})
	user := gocql.TimeUUID()
	s := prekeyClaimService{domain: "local.example", remote: &client,
		guard: func(context.Context, gocql.UUID, map[string][]string, string) error { return nil },
		claim: func(context.Context, gocql.UUID, string) (*uncheckedPrekey, error) {
			localClaims.Add(1)
			return &uncheckedPrekey{ID: 7, Key: "synthetic"}, nil
		},
	}
	domains := map[string]map[string][]string{"local.example": {user.String(): {"a"}}}
	for i := range 9 {
		domains["d"+strconv.Itoa(i)+".example"] = map[string][]string{}
	}
	done := make(chan error, 1)
	go func() { _, _, err := s.multiQualified(ctx, user, domains, "synthetic", false); done <- err }()
	for range 8 {
		select {
		case <-arrived:
		case <-ctx.Done():
			t.Fatal("first eight requests did not arrive")
		}
	}
	if active.Load() != 8 {
		t.Fatalf("pool did not admit eight: %d", active.Load())
	}
	select {
	case domain := <-arrived:
		t.Fatalf("ninth request admitted early: %s", domain)
	default:
	}
	releaseFirst.Do(func() { close(first) })
	select {
	case <-arrived:
	case <-ctx.Done():
		t.Fatal("ninth request did not follow completed failure")
	}
	select {
	case err := <-done:
		t.Fatalf("failed before remaining calls joined: %v", err)
	default:
	}
	releaseRest.Do(func() { close(rest) })
	var err error
	select {
	case err = <-done:
	case <-ctx.Done():
		t.Fatal("pool did not join")
	}
	var failure prekeyFederationFailure
	if !errors.As(err, &failure) || failure.code != 503 || peak.Load() != 8 || completed.Load() != 9 || localClaims.Load() != 1 {
		t.Fatalf("err=%v peak=%d completed=%d local=%d", err, peak.Load(), completed.Load(), localClaims.Load())
	}
}

func federationTestPeer(t *testing.T, handler http.HandlerFunc) prekeyFederationClient {
	t.Helper()
	server := httptest.NewUnstartedServer(handler)
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)
	server.Config.Protocols = protocols
	server.Start()
	t.Cleanup(server.Close)
	host, portText, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portText)
	c := Config{Federator: &Endpoint{Host: host, Port: port}}
	c.Settings.Domain = "local.example"
	client, close := newPrekeyFederationClient(c)
	t.Cleanup(close)
	return client
}

func TestFederationPrekeyHTTP2NegotiationAndTypedResponse(t *testing.T) {
	var negotiations atomic.Int32
	user := gocql.TimeUUID()
	client := federationTestPeer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 2 || r.Method != "POST" || r.Header.Get("Wire-Origin-Domain") != "local.example" || r.Header.Get("Wire-Origin-Request-Id") != "synthetic-request" {
			t.Errorf("incorrect federation transport: %s %s %v", r.Proto, r.Method, r.Header)
		}
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/api-version") {
			negotiations.Add(1)
			if string(body) != "[]" || r.Header.Get("X-Wire-API-Version") != "" {
				t.Error("negotiation must be unversioned unit input")
			}
			_, _ = io.WriteString(w, `{"supported":[0,1],"supported_versions":[0,2,4,999]}`)
			return
		}
		if r.URL.Path != "/rpc/remote.example/brig/claim-prekey" || r.Header.Get("X-Wire-API-Version") != "4" || r.Header.Get("Accept") != "application/json" {
			t.Errorf("incorrect RPC: %s %v", r.URL, r.Header)
		}
		var pair []string
		if json.Unmarshal(body, &pair) != nil || len(pair) != 2 || pair[0] != user.String() || pair[1] != "a" {
			t.Errorf("incorrect pair %s", body)
		}
		// Remote identifiers and unvalidated prekey text are trusted natively.
		_, _ = io.WriteString(w, `{"client":"000B","prekey":{"id":7.0,"key":"synthetic remote text"}}`)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for range 2 {
		key, err := client.single(ctx, "remote.example", user, "a", "synthetic-request")
		if err != nil || key == nil || key.Client != "b" || key.Prekey.ID != 7 {
			t.Fatalf("key=%v err=%v", key, err)
		}
	}
	if negotiations.Load() != 2 {
		t.Fatal("native negotiates per call")
	}
}

func TestFederationVersionsAndIntegerBounds(t *testing.T) {
	for _, tc := range []struct {
		body string
		want int
		fail bool
	}{
		{`{"supported":[0,1]}`, 0, false},
		{`{"supported":[0],"supported_versions":null}`, 0, false},
		{`{"supported":[0.0,1e0],"supported_versions":[4e0,3]}`, 4, false},
		{`{"supported":[],"supported_versions":[999]}`, 0, true},
		{`{"supported":null}`, 0, true},
		{`{"supported":[1.5]}`, 0, true},
	} {
		got, err := decodeFederationVersions([]byte(tc.body))
		if (err != nil) != tc.fail || (!tc.fail && got != tc.want) {
			t.Errorf("%s: %d %v", tc.body, got, err)
		}
	}
	for _, raw := range []string{"9223372036854775808", "1e1000000", "1e-1000000", "0.1"} {
		if _, err := federationInteger(json.RawMessage(raw)); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
}

func TestFederationRemoteFailureAndPartialMap(t *testing.T) {
	client := federationTestPeer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/api-version") {
			_, _ = io.WriteString(w, `{"supported":[0,1],"supported_versions":[4]}`)
		} else if strings.Contains(r.URL.Path, "/bad.example/") {
			w.WriteHeader(502)
			_, _ = io.WriteString(w, `{"code":503,"label":"synthetic-down","message":"remote unavailable","inner":{"code":500},"data":{"discard":true}}`)
		} else {
			_, _ = io.WriteString(w, `{}`)
		}
	})
	user := gocql.TimeUUID().String()
	s := prekeyClaimService{domain: "local.example", remote: &client}
	domains := map[string]map[string][]string{"bad.example": {user: {"a"}}, "good.example": {}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _, err := s.multiQualified(ctx, gocql.UUID{}, domains, "synthetic", false)
	var failure prekeyFederationFailure
	if !errors.As(err, &failure) || failure.code != 503 || failure.label != "synthetic-down" {
		t.Fatalf("error=%v", err)
	}
	data := failure.data.(map[string]string)
	if data["domain"] != "bad.example" || data["path"] != "/federation/claim-multi-prekey-bundle" || failure.inner == nil {
		t.Fatalf("failure=%v", failure)
	}
	values, failed, err := s.multiQualified(ctx, gocql.UUID{}, domains, "synthetic", true)
	if err != nil || len(values) != 1 || values["good.example"] == nil || len(failed) != 1 || failed[0]["id"] != user || failed[0]["domain"] != "bad.example" {
		t.Fatalf("values=%v failed=%v err=%v", values, failed, err)
	}
}

func TestIncomingFederationPrekeyNullableAndAdmission(t *testing.T) {
	user := gocql.TimeUUID()
	guards, claims := 0, 0
	s := prekeyClaimService{maxUsers: 0, federationGuard: func(_ context.Context, users map[string][]string, rid string) error {
		guards++
		if users[user.String()][0] != "a" || rid != "synthetic" {
			t.Error("guard inputs changed")
		}
		return nil
	}, claim: func(context.Context, gocql.UUID, string) (*uncheckedPrekey, error) { claims++; return nil, nil }, lookup: func(context.Context, gocql.UUID, string) (*storedClient, error) { return nil, nil }}
	for _, tc := range []struct {
		path, body, origin string
		status             int
	}{
		{"/federation/claim-prekey", `["` + user.String() + `","a"]`, "remote.example", 200},
		{"/federation/claim-multi-prekey-bundle", `{"` + user.String() + `":["a"]}`, "remote.example", 200},
		{"/federation/claim-prekey", `[]`, "remote.example", 400},
		{"/federation/claim-prekey", `[]`, "", 400},
		{"/federation/claim-multi-prekey-bundle", `{"invalid":[]}`, "remote.example", 400},
	} {
		r := httptest.NewRequest("POST", tc.path, strings.NewReader(tc.body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Request-Id", "synthetic")
		if tc.origin != "" {
			r.Header.Set("Wire-Origin-Domain", tc.origin)
		}
		w := httptest.NewRecorder()
		if !s.handleFederation(w, r, tc.path) || w.Code != tc.status {
			t.Fatalf("%s: %d %s", tc.path, w.Code, w.Body.String())
		}
		if tc.path == "/federation/claim-prekey" && tc.status == 200 && strings.TrimSpace(w.Body.String()) != "null" {
			t.Fatalf("missing federation key must be null: %s", w.Body.String())
		}
	}
	if guards != 2 || claims != 2 {
		t.Fatalf("invalid admission had effects: guards=%d claims=%d", guards, claims)
	}
}
