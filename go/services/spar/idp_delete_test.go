// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func TestIdPDeletionDeniesSelfBeforeStorageOrPurge(t *testing.T) {
	user, _ := gocql.ParseUUID("00000000-0000-4000-8000-000000000001")
	team, _ := gocql.ParseUUID("00000000-0000-4000-8000-000000000002")
	issuer := "https://synthetic.example/idp"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.Header.Get("Request-Id") != "synthetic-request" {
			t.Error("unexpected destructive RPC or request identity")
		}
		switch r.URL.Path {
		case "/i/teams/" + team.String() + "/members/" + user.String():
			_ = json.NewEncoder(w).Encode(map[string]any{"permissions": map[string]int{"self": 8191, "copy": 8191}})
		case "/i/users":
			if r.URL.Query().Get("ids") != user.String() || r.URL.Query().Get("includePendingInvitations") != "false" {
				t.Error("wrong account selected")
			}
			_ = json.NewEncoder(w).Encode([]any{map[string]any{"id": user, "team": team, "sso_id": map[string]string{"tenant": `<Issuer xmlns="` + assertionNS + `">` + issuer + `</Issuer>`, "subject": `<NameID xmlns="` + assertionNS + `">synthetic-user</NameID>`}}})
		default:
			t.Error("unexpected RPC")
		}
	}))
	defer server.Close()
	host, port, _ := net.SplitHostPort(server.Listener.Addr().String())
	number, _ := strconv.Atoi(port)
	endpoint := Endpoint{host, number}
	c := Config{Brig: endpoint, Galley: endpoint}
	p := peerClient{config: c, http: server.Client()}
	info := idpConfig{Metadata: idpMetadata{Issuer: issuer}, Extra: idpExtra{Team: team}}
	for _, purge := range []bool{false, true} {
		// A nil Cassandra session makes any mutation or enumeration fail: own
		// IdP protection must precede both, even with purge explicitly requested.
		err := deleteIdP(context.Background(), c, cassandraStore{}, p, info, user.String(), "synthetic-request", purge)
		var api *apiError
		if !errors.As(err, &api) || api.Code != 409 || api.Label != "cannot-delete-own-idp" {
			t.Fatal("own IdP deletion was not denied", err)
		}
	}
}
