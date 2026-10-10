// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/apache/cassandra-gocql-driver/v2"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestAuthorizationUsesBrigTeamAndPermissionIntersection(t *testing.T) {
	user := "00000000-0000-4000-8000-000000000001"
	team := "00000000-0000-4000-8000-000000000002"
	for _, test := range []struct {
		self, copy uint64
		allowed    bool
	}{{8191, 8191, true}, {5951, 5951, true}, {8191, 1587, false}, {8191, 1025, false}, {5951, 8191, false}, {8191, 0, false}} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/i/users" {
				if r.URL.Query().Get("ids") != user || r.URL.Query().Get("includePendingInvitations") != "false" {
					t.Error("wrong account selection")
				}
				fmt.Fprintf(w, `[{"team":%q,"deleted":false}]`, team)
				return
			}
			if r.URL.Path != "/i/teams/"+team+"/members/"+user || r.Header.Get("Z-User") != user {
				t.Error("wrong team authorization request")
			}
			fmt.Fprintf(w, `{"permissions":{"self":%d,"copy":%d}}`, test.self, test.copy)
		}))
		host, port, _ := net.SplitHostPort(server.Listener.Addr().String())
		number, _ := strconv.Atoi(port)
		endpoint := Endpoint{Host: host, Port: number}
		peers := peerClient{config: Config{Brig: endpoint, Galley: endpoint}, http: server.Client()}
		got, err := peers.authorize(context.Background(), user, "synthetic-request")
		if (err == nil) != test.allowed || err == nil && got.String() != team {
			t.Errorf("authorization for %d/%d", test.self, test.copy)
		}
		server.Close()
	}
}
func TestReauthenticationUsesGETAndPreservesCodeDenial(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if r.Method != "GET" || body["action"] != "create_scim_token" || r.URL.Path != "/i/users/00000000-0000-4000-8000-000000000001/reauthenticate" {
			t.Error("wrong reauthentication request")
		}
		w.WriteHeader(403)
		_, _ = w.Write([]byte(`{"label":"code-authentication-failed"}`))
	}))
	defer server.Close()
	host, port, _ := net.SplitHostPort(server.Listener.Addr().String())
	number, _ := strconv.Atoi(port)
	peers := peerClient{config: Config{Brig: Endpoint{Host: host, Port: number}}, http: server.Client()}
	err := peers.reauthenticate(context.Background(), "00000000-0000-4000-8000-000000000001", "synthetic-request", nil, nil)
	var api *apiError
	if !errors.As(err, &api) || api.Code != 403 || api.Label != "code-authentication-failed" {
		t.Fatal("reauthentication failure accepted or misclassified")
	}
}
func TestLegacyTokenWinsRegardlessOfCassandraRowOrder(t *testing.T) {
	legacyID, _ := gocql.ParseUUID("00000000-0000-4000-8000-000000000001")
	hashedID, _ := gocql.ParseUUID("00000000-0000-4000-8000-000000000002")
	plain := tokenRow{key: "synthetic-token", info: tokenInfo{ID: legacyID}}
	hashed := tokenRow{key: tokenHash(plain.key), info: tokenInfo{ID: hashedID}}
	for _, rows := range [][]tokenRow{{plain, hashed}, {hashed, plain}} {
		row, convert := chooseToken(rows)
		if row == nil || !convert || row.info.ID != legacyID {
			t.Fatal("hashed entry displaced the native legacy precedence")
		}
	}
	if row, _ := chooseToken([]tokenRow{hashed, hashed}); row != nil {
		t.Fatal("ambiguous lookup authorized a token")
	}
}
