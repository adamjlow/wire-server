// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func TestQueuedClientPeerCleanupAndRemovalEvent(t *testing.T) {
	for _, status := range []int{200, 204, 404, 202, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			user := gocql.TimeUUID()
			connection := "synthetic-connection"
			steps := []string{}
			galley := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				steps = append(steps, "galley")
				if r.Method != http.MethodDelete || r.URL.Path != "/i/clients/a" || r.Header.Get("Z-User") != user.String() {
					t.Error("Galley delete contract")
				}
				w.WriteHeader(status)
			}))
			defer galley.Close()
			gundeck := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodDelete {
					steps = append(steps, "gundeck")
					if r.URL.Path != "/i/clients/a" || r.Header.Get("Z-User") != user.String() {
						t.Error("Gundeck delete contract")
					}
					w.WriteHeader(204)
					return
				}
				steps = append(steps, "event")
				var pushes []map[string]json.RawMessage
				if r.URL.Path != "/i/push/v2" || json.NewDecoder(r.Body).Decode(&pushes) != nil || len(pushes) != 1 {
					t.Error("client remove event envelope")
					return
				}
				var payload []map[string]json.RawMessage
				if json.Unmarshal(pushes[0]["payload"], &payload) != nil || len(payload) != 1 || string(payload[0]["type"]) != `"user.client-remove"` || string(payload[0]["client"]) != `{"id":"a"}` || string(pushes[0]["origin_connection"]) != `"synthetic-connection"` {
					t.Error("native removal payload")
				}
				w.WriteHeader(200)
			}))
			defer gundeck.Close()
			p := brigPeers{client: galley.Client(), galley: deletionPeerEndpoint(galley), gundeck: deletionPeerEndpoint(gundeck)}
			err := p.deleteQueuedClient(context.Background(), user, "000A", &connection, "synthetic-request")
			accepted := status == 200 || status == 204 || status == 404
			if (err == nil) != accepted {
				t.Fatal("status admission", err)
			}
			if accepted && !reflect.DeepEqual(steps, []string{"galley", "gundeck", "event"}) {
				t.Fatal("native cleanup order", steps)
			}
			if !accepted {
				for _, step := range steps {
					if step != "galley" {
						t.Fatal("advanced after failure", steps)
					}
				}
			}
			if status == 500 && len(steps) != 4 {
				t.Fatal("native replayable retry count", steps)
			}
		})
	}
}
