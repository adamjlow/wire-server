// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestOriginalIdPGoldensRoundTrip(t *testing.T) {
	for _, name := range []string{"testObject_IdP_1.json", "testObject_IdP_2.json", "testObject_IdP_3.json"} {
		encoded, err := os.ReadFile("../../../libs/wire-api/test/golden/" + name)
		if err != nil {
			t.Fatal(err)
		}
		var config idpConfig
		if err = json.Unmarshal(encoded, &config); err != nil {
			t.Fatal(err)
		}
		if name == "testObject_IdP_2.json" && config.version() != 2 {
			t.Fatal("native JSON V2 enum lost its storage/query version")
		}
		actual, err := json.Marshal(config)
		if err != nil {
			t.Fatal(err)
		}
		var want, got any
		if json.Unmarshal(encoded, &want) != nil || json.Unmarshal(actual, &got) != nil || !reflect.DeepEqual(want, got) {
			t.Fatalf("original golden changed: %s", name)
		}
	}
}
func TestMetadataNamespaceBindingAndSigningKeys(t *testing.T) {
	encoded, err := os.ReadFile("../../../libs/wire-api/test/golden/testObject_IdP_1.json")
	if err != nil {
		t.Fatal(err)
	}
	var original idpConfig
	if err = json.Unmarshal(encoded, &original); err != nil {
		t.Fatal(err)
	}
	key := renderCertificate(original.Metadata.Certificates[0])
	doc := `<EntityDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata" entityID="https://synthetic.example/idp"><IDPSSODescriptor><KeyDescriptor use="encryption">` + key + `</KeyDescriptor><KeyDescriptor use="signing">` + key + `</KeyDescriptor><SingleSignOnService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Post" Location="https://synthetic.example/login"/></IDPSSODescriptor></EntityDescriptor>`
	metadata, err := parseIdPMetadata(doc)
	if err != nil || len(metadata.Certificates) != 1 || metadata.Issuer != "https://synthetic.example/idp" {
		t.Fatal("native metadata selection failed", err)
	}
	if renderCertificate(metadata.Certificates[0]) != key {
		t.Fatal("native certificate encoding changed")
	}
	for _, bad := range []string{strings.ReplaceAll(doc, "urn:oasis:names:tc:SAML:2.0:metadata", "wrong"), strings.ReplaceAll(doc, "HTTP-Post", "HTTP-Redirect"), strings.ReplaceAll(doc, "use=\"signing\"", "use=\"encryption\""), strings.Replace(doc, "</IDPSSODescriptor>", `<SingleSignOnService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST" Location="https://other.example/"/></IDPSSODescriptor>`, 1), doc + doc} {
		if _, err = parseIdPMetadata(bad); err == nil {
			t.Fatal("malformed/ambiguous metadata accepted")
		}
	}
	// The source fixture's signing certificate is expired. Metadata/store parsing
	// must preserve it; SAML signature/replay validation is a separate boundary.
	if metadata.Certificates[0].NotAfter.Year() != 2020 {
		t.Fatal("expired source certificate changed")
	}
}
func TestMultiIngressRequiresAllowlistedCertificates(t *testing.T) {
	encoded, err := os.ReadFile("../../../libs/wire-api/test/golden/testObject_IdP_1.json")
	if err != nil {
		t.Fatal(err)
	}
	var config idpConfig
	if err = json.Unmarshal(encoded, &config); err != nil {
		t.Fatal(err)
	}
	if checkIdPCertificates(Config{}, config.Metadata) != nil {
		t.Fatal("single-ingress empty allowlist changed")
	}
	multi := Config{SAML: SAMLConfig{Domains: map[string]DomainConfig{"synthetic.example": {}}}}
	if checkIdPCertificates(multi, config.Metadata) == nil {
		t.Fatal("multi-ingress empty allowlist granted permission")
	}
	multi.Fingerprints = []string{strings.Repeat("00", 20)}
	if checkIdPCertificates(multi, config.Metadata) == nil {
		t.Fatal("unlisted signing key accepted")
	}
}
