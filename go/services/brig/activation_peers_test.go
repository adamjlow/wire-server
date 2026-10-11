// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func TestActivateBindingTeamNativeRequests(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		status int
		want   int
	}{
		{"team", 200, 2}, {"no-team", 404, 1}, {"forbidden", 403, 1}, {"server-failure", 503, 4},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			calls := 0
			team := gocql.UUID{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Header.Get("Request-Id") != "synthetic-team-activation" {
					t.Error("missing request ID")
				}
				if r.Method == http.MethodGet {
					if r.URL.Path != "/i/users/"+team.String()+"/team" {
						t.Error("wrong binding lookup")
					}
					w.WriteHeader(scenario.status)
					if scenario.status == 200 {
						json.NewEncoder(w).Encode(team.String())
					}
				} else {
					var body map[string]any
					if r.Method != http.MethodPut || r.URL.Path != "/i/teams/"+team.String()+"/status" || json.NewDecoder(r.Body).Decode(&body) != nil || len(body) != 1 || body["status"] != "active" || r.Header.Get("Content-Type") != "application/json" {
						t.Error("wrong activation update")
					}
					w.WriteHeader(204)
				}
			}))
			defer server.Close()
			host, port, _ := net.SplitHostPort(server.Listener.Addr().String())
			number, _ := strconv.Atoi(port)
			peer := brigPeers{client: server.Client(), galley: Endpoint{Host: host, Port: number}}
			err := peer.activateBindingTeam(context.Background(), team, "synthetic-team-activation")
			if (err != nil) != (scenario.status >= 400 && scenario.status != 404) || calls != scenario.want {
				t.Fatalf("error=%v calls=%d", err, calls)
			}
		})
	}
}
