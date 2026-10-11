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
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func TestSessionRefreshSendsNativeChunkedDirectEventWithoutRetry(t *testing.T) {
	requests := make(chan bool, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var pushes []struct {
			Origin     string
			Recipients []struct {
				User    string `json:"user_id"`
				Route   string
				Clients []string
			}
			Payload []struct{ Type string }
			Cells   bool `json:"is_cells_event"`
		}
		valid := json.NewDecoder(r.Body).Decode(&pushes) == nil && len(pushes) == 1 && pushes[0].Origin == (gocql.UUID{}).String() && len(pushes[0].Recipients) == 1 && pushes[0].Recipients[0].Route == "direct" && pushes[0].Recipients[0].User == pushes[0].Origin && pushes[0].Recipients[0].Clients != nil && len(pushes[0].Recipients[0].Clients) == 0 && len(pushes[0].Payload) == 1 && pushes[0].Payload[0].Type == "user.session-refresh-suggested" && !pushes[0].Cells && r.Method == "POST" && r.URL.Path == "/i/push/v2" && r.Header.Get("Request-Id") == "synthetic-notification" && len(r.TransferEncoding) == 1 && r.TransferEncoding[0] == "chunked"
		requests <- valid
		w.WriteHeader(503)
	}))
	defer server.Close()
	host, port, _ := net.SplitHostPort(server.Listener.Addr().String())
	number, _ := strconv.Atoi(port)
	peers := brigPeers{client: server.Client(), gundeck: Endpoint{Host: host, Port: number}}
	notifications := newSessionNotifications(context.Background(), peers)
	defer notifications.close()
	if err := notifications.submit(context.Background(), gocql.UUID{}, "synthetic-notification"); err != nil {
		t.Fatal("best-effort downstream failure reached caller", err)
	}
	select {
	case valid := <-requests:
		if !valid {
			t.Fatal("native event wire contract changed")
		}
	case <-time.After(time.Second):
		t.Fatal("notification not delivered")
	}
	notifications.close()
	select {
	case <-requests:
		t.Fatal("native nonreplayable request retried")
	default:
	}
}
