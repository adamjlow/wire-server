// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func TestClientProjectionOriginalGolden(t *testing.T) {
	for _, name := range []string{"testObject_Client_user_1.json", "testObject_Client_user_2.json"} {
		data, err := os.ReadFile("../../../libs/wire-api/test/golden/" + name)
		if err != nil {
			t.Fatal(err)
		}
		var expected map[string]any
		if err := json.Unmarshal(data, &expected); err != nil {
			t.Fatal(err)
		}
		created, err := time.Parse(time.RFC3339Nano, expected["time"].(string))
		if err != nil {
			t.Fatal(err)
		}
		client := storedClient{ID: expected["id"].(string), Created: created}
		if expected["type"] == "permanent" {
			client.Type = 1
		} else {
			client.Type = 2
		}
		if class, exists := expected["class"]; exists {
			if class != "desktop" {
				t.Fatal("unexpected original fixture class")
			}
			value := int32(2)
			client.Class = &value
		}
		for field, pointer := range map[string]**string{"label": &client.Label, "cookie": &client.Cookie, "model": &client.Model} {
			if value, exists := expected[field]; exists {
				text := value.(string)
				*pointer = &text
			}
		}
		object, err := client.wire(8)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(object)
		if err != nil {
			t.Fatal(err)
		}
		var actual map[string]any
		if json.Unmarshal(encoded, &actual) != nil || !reflect.DeepEqual(actual, expected) {
			t.Fatalf("upstream golden projection mismatch: %s", name)
		}
	}
}

func TestClientVersionedCapabilitiesAndPublicKeys(t *testing.T) {
	client := storedClient{ID: "000FF", Type: 1, Created: time.Unix(1000, 123000000), Capabilities: []int32{2, 1, 2}, PublicKeys: map[string][]byte{"ed25519": {0, 255}}}
	for _, test := range []struct {
		version      int
		capabilities string
	}{
		{0, `{"capabilities":["legalhold-implicit-consent"]}`}, {6, `{"capabilities":["legalhold-implicit-consent"]}`},
		{7, `["legalhold-implicit-consent"]`}, {8, `["legalhold-implicit-consent","consumable-notifications"]`},
	} {
		object, err := client.wire(test.version)
		if err != nil {
			t.Fatal(err)
		}
		caps, _ := json.Marshal(object["capabilities"])
		if string(caps) != test.capabilities || object["id"] != "ff" || object["time"] != "1970-01-01T00:16:40.123Z" {
			t.Fatal("client version/normalization changed", test.version)
		}
		keys, _ := json.Marshal(object["mls_public_keys"])
		if string(keys) != `{"ed25519":"AP8="}` {
			t.Fatal("public key base64 changed")
		}
	}
	client.Capabilities = []int32{999}
	if _, err := client.wire(8); err == nil {
		t.Fatal("invalid stored capability ignored")
	}
}

func TestClientIDNativeHexBoundsAndLeadingZeros(t *testing.T) {
	for input, want := range map[string]string{"0": "0", "000001": "1", "ABCDEF": "abcdef", "FFFFFFFFFFFFFFFF": "ffffffffffffffff", strings.Repeat("0", 100) + "ff": "ff"} {
		if got, err := parseClientID(input); err != nil || got != want {
			t.Fatal("valid native client ID rejected", err)
		}
	}
	for _, input := range []string{"", "0x1", "-1", "+1", "ffjunk", "10000000000000000", " f", "f ", "f\n"} {
		if _, err := parseClientID(input); err == nil {
			t.Fatal("invalid client ID accepted")
		}
	}
}

type memoryClientReads struct {
	client    *storedClient
	requested string
}

func (s *memoryClientReads) get(_ context.Context, _ gocql.UUID, id string) (*storedClient, error) {
	s.requested = id
	return s.client, nil
}
func (s *memoryClientReads) list(context.Context, gocql.UUID) ([]storedClient, error) {
	if s.client == nil {
		return []storedClient{}, nil
	}
	return []storedClient{*s.client}, nil
}
func (s *memoryClientReads) prekeyIDs(_ context.Context, _ gocql.UUID, id string) ([]uint16, error) {
	s.requested = id
	return []uint16{}, nil
}

func TestClientReadsLegacyNotFoundAndVersioning(t *testing.T) {
	store := &memoryClientReads{}
	h := newHandler(Config{}, &brigReadHandlers{clients: &clientReadHandlers{store: store}})
	for _, test := range []struct {
		path   string
		status int
		body   string
	}{
		{"/clients", 200, "[]\n"}, {"/clients/000a", 404, ""}, {"/clients/a/prekeys", 200, "[]\n"},
		{"/clients/a/capabilities", 404, "{\"code\":404,\"label\":\"client-not-found\",\"message\":\"Client not found\"}\n"},
	} {
		req := httptest.NewRequest("GET", test.path, nil)
		req.Header.Set("Z-User", gocql.UUID{}.String())
		response := httptest.NewRecorder()
		h.ServeHTTP(response, req)
		if response.Code != test.status || response.Body.String() != test.body {
			t.Fatalf("client route mismatch %s: %d %s", test.path, response.Code, response.Body.String())
		}
	}
	store.client = &storedClient{ID: "a", Type: 1, Created: time.Unix(1000, 0), Capabilities: []int32{2, 1}}
	for _, test := range []struct{ path, body string }{{"/v6/clients/a/capabilities", "{\"capabilities\":[\"legalhold-implicit-consent\"]}\n"}, {"/v7/clients/a/capabilities", "[\"legalhold-implicit-consent\"]\n"}, {"/v8/clients/a/capabilities", "[\"legalhold-implicit-consent\",\"consumable-notifications\"]\n"}} {
		req := httptest.NewRequest("GET", test.path, nil)
		req.Header.Set("Z-User", gocql.UUID{}.String())
		response := httptest.NewRecorder()
		h.ServeHTTP(response, req)
		if response.Code != 200 || response.Body.String() != test.body {
			t.Fatal("versioned capability response changed")
		}
	}
}
