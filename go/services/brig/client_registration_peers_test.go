// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func TestNativeClientRegistrationPeerRequests(t *testing.T) {
	user := gocql.UUID{1}
	connection := "synthetic-connection"
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPost || r.Header.Get("Request-Id") != "synthetic-client" {
			t.Error("request context changed")
		}
		if calls == 1 {
			body, _ := io.ReadAll(r.Body)
			if r.URL.Path != "/i/clients/a" || r.Header.Get("Z-User") != user.String() || len(body) != 0 {
				t.Error("native Galley client request changed")
			}
			w.WriteHeader(204)
			return
		}
		if r.URL.Path != "/i/push/v2" || r.Header.Get("Z-User") != "" || r.Header.Get("Content-Type") != "application/json" || len(r.TransferEncoding) != 1 || r.TransferEncoding[0] != "chunked" {
			t.Error("native push transport changed")
		}
		var pushes []map[string]json.RawMessage
		if json.NewDecoder(r.Body).Decode(&pushes) != nil || len(pushes) != 1 {
			t.Error("invalid push")
			return
		}
		push := pushes[0]
		if string(push["origin_connection"]) != `"synthetic-connection"` || string(push["origin"]) != `"`+user.String()+`"` || string(push["is_cells_event"]) != "false" {
			t.Error("origin fields changed")
		}
		var recipients []map[string]json.RawMessage
		_ = json.Unmarshal(push["recipients"], &recipients)
		if len(recipients) != 1 || string(recipients[0]["route"]) != `"any"` || string(recipients[0]["clients"]) != "[]" {
			t.Error("client event must target all clients with RouteAny")
		}
		var payload []map[string]json.RawMessage
		_ = json.Unmarshal(push["payload"], &payload)
		var client map[string]json.RawMessage
		if len(payload) != 1 || string(payload[0]["type"]) != `"user.client-add"` || json.Unmarshal(payload[0]["client"], &client) != nil || string(client["capabilities"]) != `{"capabilities":["legalhold-implicit-consent"]}` {
			t.Error("event must use native V6 representation")
		}
		// This synchronous failure must return to addClient, without retry or
		// compensating storage writes; it cannot go through the async worker.
		w.WriteHeader(503)
	}))
	defer server.Close()
	host, port, _ := net.SplitHostPort(server.Listener.Addr().String())
	number, _ := strconv.Atoi(port)
	endpoint := Endpoint{Host: host, Port: number}
	peer := brigPeers{client: server.Client(), galley: endpoint, gundeck: endpoint}
	if err := peer.registerGalleyClient(context.Background(), user, "a", "synthetic-client"); err != nil {
		t.Fatal(err)
	}
	client := storedClient{ID: "a", Type: 1, Created: time.Unix(0, 0), Capabilities: []int32{1, 2}, PublicKeys: map[string][]byte{}}
	if err := peer.clientAddedEvent(context.Background(), user, &connection, client, "synthetic-client"); err == nil {
		t.Fatal("synchronous push failure hidden")
	}
	if calls != 2 {
		t.Fatal("unexpected retry or missing effect")
	}
}
