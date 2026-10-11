// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type testClientUpdateStore struct {
	client *storedClient
	calls  []string
	fail   string
	label  string
}

func (s *testClientUpdateStore) step(name string) error {
	s.calls = append(s.calls, name)
	if name == s.fail {
		return errors.New("synthetic failure")
	}
	return nil
}
func (s *testClientUpdateStore) get(context.Context, gocql.UUID, string) (*storedClient, error) {
	return s.client, s.step("lookup")
}
func (s *testClientUpdateStore) updateLabel(_ context.Context, _ gocql.UUID, _, label string) error {
	if err := s.step("label"); err != nil {
		return err
	}
	s.label = label
	return nil
}
func (s *testClientUpdateStore) updateCapabilities(context.Context, gocql.UUID, string, []int32) error {
	return s.step("caps")
}
func (s *testClientUpdateStore) updatePrekeys(context.Context, gocql.UUID, string, []uncheckedPrekey) error {
	return s.step("prekeys")
}
func (s *testClientUpdateStore) addMLSPublicKeys(context.Context, gocql.UUID, string, map[string][]byte) error {
	err := s.step("MLS")
	if s.fail == "duplicate" {
		return errClientMLSDuplicate
	}
	return err
}

func TestClientUpdateNativeOrderAndPartialWrites(t *testing.T) {
	label := "synthetic label"
	add := []int32{1, 2}
	remove := []int32{}
	for _, tc := range []struct {
		name    string
		request clientUpdateRequest
		missing bool
		fail    string
		want    error
		calls   []string
	}{
		{"missing", clientUpdateRequest{}, true, "", errClientMissing, []string{"lookup"}},
		{"remove after label", clientUpdateRequest{label: &label, caps: &remove}, false, "", errClientCapabilitiesRemoved, []string{"lookup", "label"}},
		{"setup before capabilities", clientUpdateRequest{label: &label, caps: &add}, false, "", nil, []string{"lookup", "label", "setup", "caps", "prekeys", "MLS"}},
		{"setup failure keeps label", clientUpdateRequest{label: &label, caps: &add}, false, "setup", nil, []string{"lookup", "label", "setup"}},
		{"malformed after caps", clientUpdateRequest{label: &label, caps: &add, prekeys: []uncheckedPrekey{{ID: 1, Key: "invalid"}}}, false, "", errPrekeyBundle, []string{"lookup", "label", "setup", "caps"}},
		{"prekey store failure", clientUpdateRequest{}, false, "prekeys", nil, []string{"lookup", "prekeys"}},
		{"duplicate after prekeys", clientUpdateRequest{}, false, "duplicate", errClientMLSDuplicate, []string{"lookup", "prekeys", "MLS"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &testClientUpdateStore{client: &storedClient{Capabilities: []int32{1}}, fail: tc.fail}
			if tc.missing {
				store.client = nil
			}
			s := clientUpdateService{store: store, consumable: true, setup: func(context.Context, gocql.UUID, string, string) error { return store.step("setup") }}
			err := s.update(context.Background(), gocql.TimeUUID(), "1", tc.request, "synthetic-id")
			if tc.fail != "" && tc.want == nil {
				if err == nil {
					t.Fatal("failure ignored")
				}
			} else if !errors.Is(err, tc.want) {
				t.Fatal(err, tc.want)
			}
			if !reflect.DeepEqual(store.calls, tc.calls) {
				t.Fatal(store.calls, tc.calls)
			}
			if tc.request.label != nil && store.label != label {
				t.Fatal("native committed label lost")
			}
		})
	}
}

func TestClientUpdateRequestNativeVersionsAndTypes(t *testing.T) {
	for _, tc := range []struct {
		raw     string
		version int
		ok      bool
	}{
		{`{}`, 19, true}, {`{"label":"first","label":"second"}`, 19, true},
		{`{"prekeys":[{"id":7.0,"key":""}],"lastkey":{"id":65535e0,"key":""}}`, 19, true},
		{`{"capabilities":["consumable-notifications","legalhold-implicit-consent","consumable-notifications"]}`, 19, true},
		{`{"capabilities":["consumable-notifications"]}`, 7, false},
		{`{"capabilities":["legalhold-implicit-consent"]}`, 6, true},
		{`{"capabilities":{"capabilities":["legalhold-implicit-consent"]}}`, 6, false},
		{`{"mls_public_keys":{"ed25519":""},"label":null,"prekeys":null,"capabilities":null}`, 19, true},
		{`{"prekeys":[{"id":65536,"key":""}]}`, 19, false}, {`{"lastkey":{"id":7,"key":""}}`, 19, false},
		{`{"prekeys":[{"id":1.5,"key":""}]}`, 19, false}, {`{"mls_public_keys":{"unknown":"AA=="}}`, 19, false},
		{`{"mls_public_keys":{"ed25519":"AB=="}}`, 19, false}, {`[]`, 19, false}, {`{} {}`, 19, false},
	} {
		request, err := decodeClientUpdate(strings.NewReader(tc.raw), tc.version)
		if (err == nil) != tc.ok {
			t.Fatal(tc.raw, err)
		}
		if tc.raw == `{"label":"first","label":"second"}` && *request.label != "first" {
			t.Fatal("first native JSON key lost")
		}
	}
}

func TestClientUpdateActualGundeckSetupAndHTTP(t *testing.T) {
	user := gocql.TimeUUID()
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if r.Method != "POST" || r.URL.Path != "/i/users/"+user.String()+"/clients/1/consumable-notifications" || r.Header.Get("Request-Id") != "synthetic-request" || r.Header.Get("Z-User") != "" {
			t.Error(r.Method, r.URL.Path, r.Header)
		}
		if attempts == 1 {
			w.WriteHeader(503)
		} else {
			w.WriteHeader(204)
		}
	}))
	defer server.Close()
	host, portText, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	endpoint := Endpoint{Host: host, Port: port}
	peer := brigPeers{client: server.Client(), gundeck: endpoint}
	store := &testClientUpdateStore{client: &storedClient{}}
	s := clientUpdateService{store: store, consumable: true, setup: peer.setupConsumableClient}
	h := newHandler(Config{}, &brigReadHandlers{clientUpdate: &s})
	r := httptest.NewRequest("PUT", "/v19/clients/1", strings.NewReader(`{"capabilities":["consumable-notifications"]}`))
	r.Header.Set("Z-User", user.String())
	r.Header.Set("Request-Id", "synthetic-request")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || w.Body.Len() != 0 || w.Header().Get("Content-Type") != "" || attempts != 2 {
		t.Fatal(w.Code, w.Body.String(), attempts)
	}
}
