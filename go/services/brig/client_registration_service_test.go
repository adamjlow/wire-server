// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
	"go.yaml.in/yaml/v3"
)

func syntheticNewClient(t *testing.T) clientRegistrationRequest {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString("pQABAQcCoQBYIDXdN8VlKb5lbgPmoDPLPyqNIEyShG4oT/DlW0peRRZUA6EAoQBYILLf1TIwSB62q69Ojs/X1tzJ+dYHNAw4QbW/7TC5vSZqBPY=")
	if err != nil {
		t.Fatal(err)
	}
	raw = append(append(append([]byte{}, raw[:4]...), 0x19, 0xff, 0xff), raw[5:]...)
	return clientRegistrationRequest{client: storedClient{Type: 0, Capabilities: []int32{2}, PublicKeys: map[string][]byte{}}, last: uncheckedPrekey{ID: 65535, Key: base64.StdEncoding.EncodeToString(raw)}}
}

func TestClientRegistrationNativeCompositionAndFailureOrder(t *testing.T) {
	for _, fail := range []string{"", "setup", "delete-old", "galley", "event", "mail"} {
		t.Run("failure-"+fail, func(t *testing.T) {
			events := []string{}
			failure := errors.New("synthetic peer failure")
			store := &registrationClientFake{clients: []storedClient{{ID: "a", Type: 0}}, events: &events}
			email := "synthetic@example.com"
			user := gocql.UUID{}
			step := func(name string) error {
				events = append(events, name)
				if name == fail {
					return failure
				}
				return nil
			}
			s := clientRegistrationService{
				core: clientRegistrationCore{store: store, maxPermanent: 7, now: func() time.Time { return time.Unix(1, 0) }, reauthenticate: func(context.Context, gocql.UUID, *string) error { return step("auth") }},
				account: func(context.Context, gocql.UUID) (*storedUser, error) {
					events = append(events, "account")
					return &storedUser{ID: user, Name: "Synthetic", Email: &email, Activated: true}, nil
				},
				consumable: true, setup: func(context.Context, gocql.UUID, string, string) error { return step("setup") },
				deleteOld: func(context.Context, gocql.UUID, *string, storedClient) error { return step("delete-old") },
				galley:    func(context.Context, gocql.UUID, string, string) error { return step("galley") },
				event:     func(context.Context, gocql.UUID, *string, storedClient, string) error { return step("event") },
				mail:      func(context.Context, selfProfile, storedClient) error { return step("mail") }, domain: "synthetic.example", locale: "en",
			}
			_, err := s.addPublic(context.Background(), user, "synthetic", syntheticNewClient(t), "synthetic-request")
			want := []string{"account", "account", "account", "lookup", "auth", "insert", "setup", "delete-old", "galley", "event", "mail"}
			if fail != "" {
				for i, name := range want {
					if name == fail {
						want = want[:i+1]
						break
					}
				}
			}
			if (err == nil) != (fail == "") || !reflect.DeepEqual(events, want) || store.written.ID == "" {
				t.Fatalf("error=%v events=%v", err, events)
			}
		})
	}
}

func TestClientRegistrationVerificationAndPublicPolicy(t *testing.T) {
	for _, tc := range []struct {
		name                                     string
		missing, required, code, saml, legalhold bool
		want                                     error
	}{
		{"missing account", true, false, false, false, false, errClientUserMissing},
		{"public legalhold", false, false, false, false, true, errClientLegalHoldRegistration},
		{"required code", false, true, false, false, false, errClientCodeRequired},
		{"failed code", false, true, true, false, false, errClientCodeFailed},
		{"SAML bypass", false, true, false, true, false, nil},
		{"disabled bypass", false, false, false, false, false, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			events := []string{}
			store := &registrationClientFake{events: &events}
			request := syntheticNewClient(t)
			request.client.Capabilities = nil
			if tc.legalhold {
				request.client.Type = 2
			}
			code := "synthetic-code"
			if tc.code {
				request.verificationCode = &code
			}
			email := "synthetic@example.com"
			team := gocql.UUID{1}
			calls := 0
			verified := 0
			s := clientRegistrationService{core: clientRegistrationCore{store: store, maxPermanent: 7, now: time.Now},
				account: func(context.Context, gocql.UUID) (*storedUser, error) {
					calls++
					if tc.missing {
						return nil, nil
					}
					row := &storedUser{Email: &email, Team: &team, Activated: true}
					if tc.saml {
						row.SSO = json.RawMessage(`{"tenant":"https://synthetic.example/issuer","subject":"synthetic"}`)
					}
					return row, nil
				},
				verificationEnabled: func(context.Context, string, string) (bool, error) { return tc.required, nil },
				verifyCode: func(_ context.Context, _ string, scope, _ string) (bool, error) {
					verified++
					if scope != "login" {
						t.Error("wrong verification scope")
					}
					return false, nil
				},
				galley: func(context.Context, gocql.UUID, string, string) error { return nil }, event: func(context.Context, gocql.UUID, *string, storedClient, string) error { return nil }, domain: "synthetic.example", locale: "en",
			}
			_, err := s.addPublic(context.Background(), gocql.UUID{}, "synthetic", request, "synthetic")
			if !errors.Is(err, tc.want) {
				t.Fatal(err)
			}
			if tc.legalhold && calls != 0 || tc.missing && calls != 1 || !tc.legalhold && !tc.missing && calls != 3 {
				t.Fatal("native account lookup/code work order changed")
			}
			if (verified == 1) != (tc.required && tc.code && !tc.saml) {
				t.Fatal("verification invoked in wrong branch")
			}
			if tc.want != nil && store.written.ID != "" {
				t.Fatal("failed admission wrote client")
			}
		})
	}
}

func TestPublicClientRegistrationResponseAndConfiguredLimit(t *testing.T) {
	events := []string{}
	store := &registrationClientFake{events: &events}
	s := clientRegistrationService{core: clientRegistrationCore{store: store, maxPermanent: 7, now: func() time.Time { return time.Unix(1, 0) }}, account: func(context.Context, gocql.UUID) (*storedUser, error) { return &storedUser{}, nil }, galley: func(context.Context, gocql.UUID, string, string) error { return nil }, event: func(context.Context, gocql.UUID, *string, storedClient, string) error { return nil }, domain: "synthetic.example", locale: "en"}
	request := syntheticNewClient(t)
	body, _ := json.Marshal(map[string]any{"type": "temporary", "prekeys": []any{}, "lastkey": request.last})
	r := httptest.NewRequest("POST", "/v19/clients", strings.NewReader(string(body)))
	r.Header.Set("Z-User", (gocql.UUID{}).String())
	r.Header.Set("Z-Connection", "synthetic")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	newHandler(Config{}, &brigReadHandlers{clientRegistration: &s}).ServeHTTP(w, r)
	if w.Code != 201 || w.Header().Get("Location") != clientIDFromPrekey(request.last) || w.Header().Get("Content-Type") != "application/json;charset=utf-8" {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var client map[string]json.RawMessage
	_ = json.Unmarshal(w.Body.Bytes(), &client)
	if string(client["capabilities"]) != "[]" || client["last_active"] != nil {
		t.Fatal("native new client response changed")
	}
	var c Config
	if yaml.Unmarshal([]byte("optSettings:\n  setUserMaxPermClients: 3\n"), &c) != nil || c.Settings.MaxPermanentClients == nil || *c.Settings.MaxPermanentClients != 3 {
		t.Fatal("native configured permanent limit ignored")
	}
}
