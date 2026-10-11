// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"strings"
	"testing"
)

func TestClientRegistrationRequiredAndVersionedFields(t *testing.T) {
	// The HTTP schema checks the ID/shape; native bundle validation follows
	// admission/reauthentication in the core, not request decoding.
	base := `"prekeys":[],"lastkey":{"id":65535,"key":"synthetic"},"type":"permanent"`
	for _, tc := range []struct {
		name, body string
		version    int
		valid      bool
	}{
		{"minimal", `{` + base + `}`, 19, true},
		{"missing prekeys", `{"lastkey":{"id":65535,"key":"x"},"type":"permanent"}`, 19, false},
		{"null prekeys", `{` + base + `,"prekeys":null}`, 19, true}, // native first duplicate wins
		{"first null prekeys", `{"prekeys":null,` + base + `}`, 19, false},
		{"missing lastkey", `{"prekeys":[],"type":"permanent"}`, 19, false},
		{"wrong last ID", `{"prekeys":[],"lastkey":{"id":7,"key":"x"},"type":"permanent"}`, 19, false},
		{"unknown type", `{` + strings.Replace(base, `"permanent"`, `"other"`, 1) + `}`, 19, false},
		{"legalhold decoded before public policy", `{` + strings.Replace(base, `"permanent"`, `"legalhold"`, 1) + `}`, 19, true},
		{"v6 array", `{` + base + `,"capabilities":["legalhold-implicit-consent"]}`, 6, true},
		{"v6 nested object is read only", `{` + base + `,"capabilities":{"capabilities":[]}}`, 6, false},
		{"v7 new capability rejected", `{` + base + `,"capabilities":["consumable-notifications"]}`, 7, false},
		{"v8 new capability accepted", `{` + base + `,"capabilities":["consumable-notifications"]}`, 8, true},
		{"short password", `{` + base + `,"password":"short"}`, 19, false},
		{"null optional fields", `{` + base + `,"password":null,"verification_code":null,"class":null}`, 19, true},
		{"bad code", `{` + base + `,"verification_code":"short"}`, 19, false},
		{"unknown class", `{` + base + `,"class":"other"}`, 19, false},
		{"bad MLS", `{` + base + `,"mls_public_keys":{"ed25519":"@@"}}`, 19, false},
		{"trailing", `{` + base + `} {}`, 19, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := decodeClientRegistration(strings.NewReader(tc.body), tc.version)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, err)
			}
		})
	}
	request, err := decodeClientRegistration(strings.NewReader(`{`+base+`,"capabilities":[],"class":"desktop","cookie":"synthetic-label","model":"synthetic-model","password":"secret6","mls_public_keys":{"ed25519":"AQI="}}`), 19)
	if err != nil || request.client.Capabilities == nil || request.client.Class == nil || *request.client.Class != 2 || request.client.Cookie == nil || *request.client.Cookie != "synthetic-label" || request.password == nil || string(request.client.PublicKeys["ed25519"]) != "\x01\x02" {
		t.Fatal("typed optional fields lost")
	}
	request, err = decodeClientRegistration(strings.NewReader(`{`+base+`}`), 19)
	if err != nil || request.client.Capabilities != nil {
		t.Fatal("absent capabilities must remain nullable for native CQL")
	}
}
