// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func TestPrekeyClaimGuardAndMissingCleanupOrder(t *testing.T) {
	user, protectee := gocql.TimeUUID(), gocql.TimeUUID()
	for _, mode := range []string{"key", "missing client", "cleanup", "guard failure", "claim failure", "cleanup failure"} {
		t.Run(mode, func(t *testing.T) {
			trace := []string{}
			s := prekeyClaimService{guard: func(_ context.Context, p gocql.UUID, clients map[string][]string, _ string) error {
				trace = append(trace, "guard")
				if p != protectee || len(clients[user.String()]) != 1 || clients[user.String()][0] != "a" {
					t.Fatal("guard input changed")
				}
				if mode == "guard failure" {
					return errPrekeyConsent
				}
				return nil
			}, claim: func(context.Context, gocql.UUID, string) (*uncheckedPrekey, error) {
				trace = append(trace, "claim")
				if mode == "claim failure" {
					return nil, errors.New("synthetic store failure")
				}
				if mode == "key" {
					return &uncheckedPrekey{ID: 7, Key: "synthetic"}, nil
				}
				return nil, nil
			}, lookup: func(context.Context, gocql.UUID, string) (*storedClient, error) {
				trace = append(trace, "lookup")
				if mode == "missing client" {
					return nil, nil
				}
				return &storedClient{ID: "a"}, nil
			}, remove: func(_ context.Context, u gocql.UUID, connection *string, c storedClient) error {
				trace = append(trace, "delete")
				if connection != nil || u != user || c.ID != "a" {
					t.Fatal("native noPrekeys deletion changed")
				}
				if mode == "cleanup failure" {
					return errors.New("synthetic cleanup failure")
				}
				return nil
			}}
			key, err := s.single(context.Background(), protectee, user, "a", "synthetic")
			want := map[string]string{"key": "guard,claim", "missing client": "guard,claim,lookup", "cleanup": "guard,claim,lookup,delete", "guard failure": "guard", "claim failure": "guard,claim", "cleanup failure": "guard,claim,lookup,delete"}[mode]
			if strings.Join(trace, ",") != want || (err != nil) != strings.HasSuffix(mode, "failure") || (key != nil) != (mode == "key") {
				t.Fatalf("trace=%v key=%v err=%v", trace, key, err)
			}
		})
	}
	s := prekeyClaimService{clients: func(context.Context, gocql.UUID) ([]storedClient, error) {
		return []storedClient{{ID: "a"}, {ID: "b"}}, nil
	}, guard: func(_ context.Context, _ gocql.UUID, c map[string][]string, _ string) error {
		if strings.Join(c[user.String()], ",") != "a,b" {
			t.Fatal("bundle guard lost client set")
		}
		return nil
	}, claim: func(_ context.Context, _ gocql.UUID, id string) (*uncheckedPrekey, error) {
		if id == "a" {
			return nil, nil
		}
		return &uncheckedPrekey{ID: 65535, Key: "synthetic"}, nil
	}, remove: func(context.Context, gocql.UUID, *string, storedClient) error {
		t.Fatal("bundle must not delete client with no prekeys")
		return nil
	}}
	keys, err := s.bundle(context.Background(), protectee, user, "synthetic")
	if err != nil || len(keys) != 1 {
		t.Fatal("native bundle missing-key omission changed")
	}
}

func TestPrekeyLegalholdActualHTTP(t *testing.T) {
	user := gocql.TimeUUID()
	status := 200
	label := ""
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "PUT" || r.URL.Path != "/i/guard-legalhold-policy-conflicts" || r.Header.Get("Request-Id") != "synthetic" || r.Header.Get("Z-User") != "" || r.Header.Get("Content-Type") != "application/json" {
			t.Error("guard HTTP contract changed")
		}
		var body struct {
			GlhProtectee   struct{ Tag, Contents string }
			GlhUserClients map[string][]string
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || body.GlhProtectee.Tag != "ProtectedUser" || body.GlhProtectee.Contents != user.String() || strings.Join(body.GlhUserClients[user.String()], ",") != "a,f,10" {
			t.Error("native legalhold guard JSON changed")
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{"label": label})
	}))
	defer server.Close()
	host, port, _ := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	number, _ := strconv.Atoi(port)
	peer := brigPeers{client: server.Client(), galley: Endpoint{Host: host, Port: number}}
	for _, tc := range []struct {
		status int
		label  string
		want   error
	}{{200, "", nil}, {404, "", nil}, {403, "missing-legalhold-consent", errPrekeyConsent}, {403, "missing-legalhold-consent-old-clients", errPrekeyOldConsent}, {403, "unknown", errPrekeyConsent}, {500, "", errors.New("failure")}} {
		status, label = tc.status, tc.label
		before := calls
		err := peer.guardPrekeyLegalhold(context.Background(), user, map[string][]string{user.String(): {"10", "000A", "f", "a"}}, "synthetic")
		if tc.status == 500 {
			if err == nil || calls-before != 4 {
				t.Fatal("native RPC retries/failure changed")
			}
			continue
		}
		if !errors.Is(err, tc.want) || calls-before != 1 {
			t.Fatalf("status%d error%v", tc.status, err)
		}
	}
}

func TestPrekeyLocalRoutes(t *testing.T) {
	user := gocql.TimeUUID()
	claims := 0
	s := prekeyClaimService{domain: "example.com", claim: func(context.Context, gocql.UUID, string) (*uncheckedPrekey, error) {
		claims++
		return &uncheckedPrekey{ID: 7, Key: "synthetic"}, nil
	}, guard: func(context.Context, gocql.UUID, map[string][]string, string) error { return nil }, clients: func(context.Context, gocql.UUID) ([]storedClient, error) { return []storedClient{}, nil }}
	h := newHandler(Config{}, &brigReadHandlers{prekeys: &s})
	for _, tc := range []struct {
		path   string
		status int
	}{{"/v19/users/example.com/" + user.String() + "/prekeys/000A", 200}, {"/v1/users/" + user.String() + "/prekeys/a", 200}, {"/v2/users/" + user.String() + "/prekeys/a", 404}, {"/v19/users/example.com/" + user.String() + "/prekeys", 200}, {"/v19/users/remote.example/" + user.String() + "/prekeys/a", 400}, {"/v19/users/example.com/invalid/prekeys/a", 400}} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", tc.path, nil)
		r.Header.Set("Z-User", user.String())
		h.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("%s: %d %s", tc.path, w.Code, w.Body.String())
		}
		if strings.HasSuffix(tc.path, "/000A") && !strings.Contains(w.Body.String(), `"client":"a"`) {
			t.Fatal("client ID not normalized")
		}
	}
	if claims != 2 {
		t.Fatal("excluded version/remote route claimed local key")
	}
}
