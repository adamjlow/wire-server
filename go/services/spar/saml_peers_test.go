// SPDX-License-Identifier: AGPL-3.0-only
package spar

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

func TestBrigSSOLoginUsesBackendCookie(t *testing.T) {
	user, err := gocql.ParseUUID("00000000-0000-4000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	label := "synthetic-cookie-label"
	first := "zuid=synthetic; Expires=Wed, 21 Oct 2026 07:28:00 GMT; HttpOnly; Secure"
	for _, tc := range []struct {
		name    string
		status  int
		cookies []string
		fail    bool
	}{
		{"session", 200, []string{first, "other=second"}, false},
		{"missing-cookie", 200, nil, true},
		{"denied-with-cookie", 403, []string{first}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var payload map[string]any
				if r.Method != "POST" || r.URL.Path != "/i/sso-login" || r.URL.RawQuery != "persist=true" || r.Header.Get("Request-Id") != "synthetic-request" || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Z-User") != "" {
					t.Error("incorrect SSO RPC envelope")
				}
				if json.NewDecoder(r.Body).Decode(&payload) != nil || payload["user"] != user.String() || payload["label"] != label {
					t.Error("incorrect SSO RPC body")
				}
				for _, cookie := range tc.cookies {
					w.Header().Add("Set-Cookie", cookie)
				}
				w.WriteHeader(tc.status)
			}))
			defer server.Close()
			host, port, _ := net.SplitHostPort(server.Listener.Addr().String())
			number, _ := strconv.Atoi(port)
			p := peerClient{config: Config{Brig: Endpoint{Host: host, Port: number}}, http: server.Client()}
			cookie, err := p.ssoLogin(context.Background(), user, &label, "synthetic-request")
			if tc.fail {
				if err == nil || cookie != "" {
					t.Fatal("missing or denied session fabricated cookie")
				}
			} else if err != nil || cookie != first {
				t.Fatal("backend cookie changed or joined", err)
			}
		})
	}
}
