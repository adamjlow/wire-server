// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func TestPublicRegistrationHTTPRejectionsBeforeEffects(t *testing.T) {
	for _, test := range []struct {
		method, ip, media, body string
		status                  int
	}{
		{"GET", "127.0.0.1", "application/json", `{}`, 405},
		{"POST", "", "application/json", `{}`, 400},
		{"POST", "127.0.0.1, 127.0.0.2", "application/json", `{}`, 400},
		{"POST", "fe80::1%lo", "application/json", `{}`, 400},
		{"POST", "127.0.0.1", "text/plain", `{}`, 415},
		{"POST", "127.0.0.1", "application/json", `{"name":"Synthetic","uuid":"10000000-0000-0000-0000-000000000001"}`, 400},
		{"POST", "127.0.0.1", "application/json", `{"name":"Synthetic","sso_id":{"scim_external_id":"synthetic"}}`, 400},
		{"POST", "127.0.0.1", "application/json", `{"name":"Synthetic","label":"synthetic-public-device"}`, 403},
	} {
		request := httptest.NewRequest(test.method, "/register", strings.NewReader(test.body))
		request.Header.Set("Content-Type", test.media)
		if test.ip != "" {
			request.Header.Set("X-Forwarded-For", test.ip)
		}
		recorder := httptest.NewRecorder()
		(registrationHTTP{}).handlePublic(recorder, request)
		if recorder.Code != test.status || recorder.Header().Get("Set-Cookie") != "" {
			t.Fatalf("wrong rejection/cookie: %d %s", recorder.Code, recorder.Body.String())
		}
	}
}

func TestPublicRegistrationHTTPActualMailAndSession(t *testing.T) {
	for _, ephemeral := range []bool{true, false} {
		t.Run(strconv.FormatBool(ephemeral), func(t *testing.T) {
			mailServer := newSMTPFixture(t, "plain")
			config := nativeMailTestConfig(t)
			smtp := mailServer.config()
			config.Other["emailSMS"].(map[string]any)["email"] = map[string]any{"smtpEndpoint": map[string]any{"host": smtp.endpoint.Host, "port": smtp.endpoint.Port}, "smtpConnType": "plain"}
			delivery, err := newRegistrationMailDelivery(context.Background(), config, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer delivery.close()
			peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.Path != "/v19/conversations/self" || r.Header.Get("Request-Id") != "synthetic-request" || r.Header.Get("Z-User") == "" {
					t.Error("request-scoped Galley call changed")
				}
				w.WriteHeader(200)
			}))
			defer peer.Close()
			parsed, _ := url.Parse(peer.URL)
			host, port, _ := net.SplitHostPort(parsed.Host)
			number, _ := strconv.Atoi(port)
			sessions, store, _ := testSessionManager()
			writes := 0
			service := registrationService{domain: "example.com", locale: registrationLocale{Language: "en"}, sessionTTL: 3600, activationTimeout: 600, now: time.Now,
				guardDomain: func(context.Context, string) error { return nil },
				identity:    registrationIdentity{owner: func(context.Context, string) (*gocql.UUID, error) { return nil, nil }, account: func(context.Context, gocql.UUID) (*storedUser, error) { return nil, nil }, deleteKey: func(context.Context, string, *gocql.UUID) error { return nil }, blacklisted: func(context.Context, string) (bool, error) { return false, nil }},
				hash: func(_ context.Context, key passwordRateKey, _ string, _ int) (string, error) {
					if key.kind != "ip" || key.ip.String() != "127.0.0.42" {
						t.Fatal("public hashing rate key changed")
					}
					return "synthetic-hash", nil
				},
				createAccount: func(context.Context, storedUser) error { writes++; return nil },
				newCode: func(context.Context, string, float64, *gocql.UUID) (activationRecord, error) {
					return activationRecord{key: "synthetic-key", code: "123456"}, nil
				},
				initFeature: func(context.Context, gocql.UUID) error { return nil },
			}
			registration := registrationHTTP{policy: registrationPolicy{ephemeral: true}, mail: delivery, sessions: sessions, refreshSuggested: func(context.Context, gocql.UUID, string) error {
				t.Fatal("fresh signup unexpectedly revoked a session")
				return nil
			}, service: service, activation: &activationHTTP{}, peers: brigPeers{client: peer.Client(), galley: Endpoint{Host: host, Port: number}}, galleyVersion: 19}
			business := brigReadHandlers{registration: &registration}
			body := `{"name":"Synthetic","label":"synthetic-public-device"}`
			if !ephemeral {
				body = `{"name":"Synthetic","email":"synthetic@example.com","password":"12345678","label":"synthetic-public-device"}`
			}
			request := httptest.NewRequest("POST", "/v19/register", strings.NewReader(body))
			request.Header.Set("X-Forwarded-For", "127.0.0.42")
			request.Header.Set("Request-Id", "synthetic-request")
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			newHandler(Config{}, &business).ServeHTTP(recorder, request)
			var profile selfProfile
			if recorder.Code != 201 || json.Unmarshal(recorder.Body.Bytes(), &profile) != nil {
				t.Fatalf("registration response: %d %s", recorder.Code, recorder.Body.String())
			}
			cookies := recorder.Result().Cookies()
			if len(cookies) != 1 || cookies[0].Name != "zuid" || cookies[0].Path != "/access" || !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].Expires.IsZero() != ephemeral {
				t.Fatal("native session/persistent cookie attributes changed")
			}
			if len(store.cookies) != 1 || writes != 1 || recorder.Header().Get("Location") != profile.ID || recorder.Header().Get("Content-Type") != "application/json;charset=utf-8" || strings.Contains(recorder.Body.String(), "access_token") {
				t.Fatal("native registration profile/location/cookie response changed")
			}
			if !ephemeral {
				select {
				case mail := <-mailServer.received:
					if !strings.Contains(mail, "X-Zeta-Purpose: Activation") {
						t.Fatal("activation mail missing")
					}
				default:
					t.Fatal("registration did not await actual mail")
				}
			}
		})
	}
}
