// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"testing"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func deletionPeerEndpoint(server *httptest.Server) Endpoint {
	host, port, _ := net.SplitHostPort(server.Listener.Addr().String())
	number, _ := strconv.Atoi(port)
	return Endpoint{Host: host, Port: number}
}

func TestDeletionOwnerNativeStatus(t *testing.T) {
	for _, status := range []int{200, 204, 403, 404, 500} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			user, team := gocql.TimeUUID(), gocql.TimeUUID()
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodGet || r.URL.Path != "/i/teams/"+team.String()+"/is-team-owner/"+user.String() || r.Header.Get("Request-Id") != "synthetic-delete" || r.Header.Get("Z-User") != "" {
					t.Error("incorrect owner request")
				}
				w.WriteHeader(status)
			}))
			defer server.Close()
			p := accountDeletionPeers{peers: brigPeers{client: server.Client(), galley: deletionPeerEndpoint(server)}}
			owner, err := p.owner(context.Background(), team, user, "synthetic-delete")
			if err != nil || owner != (status != 403) || calls != 1 {
				t.Fatalf("owner=%v error=%v calls=%d", owner, err, calls)
			}
		})
	}
}

func TestDeletionPeerOrderAndFailures(t *testing.T) {
	for _, scenario := range []struct {
		name            string
		gundeck, galley int
		assets          []int
		want            []string
		fail            bool
	}{
		{"assets-best-effort", 204, 200, []int{404, 503}, []string{"gundeck", "galley", "asset0", "asset1"}, false},
		{"gundeck-retry-exhausted", 503, 204, []int{204}, []string{"gundeck", "gundeck", "gundeck", "gundeck"}, true},
		{"galley-forbidden", 204, 403, []int{204}, []string{"gundeck", "galley"}, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			user := storedUser{ID: gocql.TimeUUID()}
			for range scenario.assets {
				user.Assets = append(user.Assets, userAsset{Key: "3-1-" + gocql.TimeUUID().String()})
			}
			var calls []string
			serve := func(name string, status int) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls = append(calls, name)
					if r.Method != http.MethodDelete || r.URL.Path != "/i/user" || r.Header.Get("Z-User") != user.ID.String() || r.Header.Get("Request-Id") != "synthetic-delete" {
						t.Error("incorrect cleanup request")
					}
					w.WriteHeader(status)
				}))
			}
			gundeck, galley := serve("gundeck", scenario.gundeck), serve("galley", scenario.galley)
			defer gundeck.Close()
			defer galley.Close()
			asset := 0
			cargohold := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if asset >= len(user.Assets) {
					t.Error("unexpected asset retry")
					w.WriteHeader(500)
					return
				}
				calls = append(calls, "asset"+strconv.Itoa(asset))
				if r.Method != http.MethodDelete || r.URL.Path != "/assets/v3/"+user.Assets[asset].Key || r.Header.Get("Z-User") != user.ID.String() || r.Header.Get("Request-Id") != "synthetic-delete" {
					t.Error("incorrect asset cleanup request")
				}
				w.WriteHeader(scenario.assets[asset])
				asset++
			}))
			defer cargohold.Close()
			p := accountDeletionPeers{peers: brigPeers{client: gundeck.Client(), gundeck: deletionPeerEndpoint(gundeck), galley: deletionPeerEndpoint(galley)}, cargohold: deletionPeerEndpoint(cargohold)}
			err := p.remove(context.Background(), user, "synthetic-delete")
			if (err != nil) != scenario.fail || !reflect.DeepEqual(calls, scenario.want) {
				t.Fatalf("error=%v calls=%v want=%v", err, calls, scenario.want)
			}
		})
	}
}

type deletionRoundTripper func(*http.Request) (*http.Response, error)

func (f deletionRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDeletionPeerValidationAndCancellation(t *testing.T) {
	calls := 0
	p := accountDeletionPeers{peers: brigPeers{client: &http.Client{Transport: deletionRoundTripper(func(*http.Request) (*http.Response, error) { calls++; return nil, io.EOF })}}}
	if p.remove(context.Background(), storedUser{Assets: []userAsset{{Key: "../../i/user"}}}, "synthetic-delete") == nil || calls != 0 {
		t.Fatal("malformed stored key caused side effects")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.owner(ctx, gocql.TimeUUID(), gocql.TimeUUID(), "synthetic-delete"); err == nil || calls > 1 {
		t.Fatal("canceled cleanup continued retries")
	}
}
