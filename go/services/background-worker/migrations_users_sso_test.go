// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestUserSSOMigrationReusesOriginalNativeJSONGoldens(t *testing.T) {
	for _, fixture := range []string{"testObject_UserSSOId_user_2.json", "testObject_UserSSOId_user_9.json", "testObject_UserSSOId_user_13.json"} {
		input, err := os.ReadFile(filepath.Join("..", "..", "..", "libs", "wire-api", "test", "golden", fixture))
		if err != nil {
			t.Fatal(err)
		}
		actual, err := decodeUserMigrationSSO(string(input))
		if err != nil {
			t.Fatalf("native golden %s: %v", fixture, err)
		}
		var got, want map[string]string
		if json.Unmarshal(actual, &got) != nil || json.Unmarshal(input, &want) != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("native golden serialization changed: %s", fixture)
		}
	}
}

func TestUserSSOMigrationCanonicalizesLegacyTextAndXML(t *testing.T) {
	input := `{"tenant":"http://example.com/","subject":"me@example.com","ignored":true}`
	canonical, err := decodeUserMigrationSSO(input)
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(filepath.Join("..", "..", "..", "libs", "wire-api", "test", "golden", "testObject_UserSSOId_user_2.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got, want map[string]string
	_ = json.Unmarshal(canonical, &got)
	_ = json.Unmarshal(original, &want)
	if !reflect.DeepEqual(got, want) {
		t.Fatal("legacy fields did not canonicalize to original XML strings")
	}
	for _, subject := range []string{"legacy <name>&\"'", `<s:NameID xmlns:s="urn:oasis:names:tc:SAML:2.0:assertion" Format="urn:oasis:names:tc:SAML:2.0:nameid-format:persistent" NameQualifier="a&amp;b" SPNameQualifier="tenant" SPProvidedID="provided">synthetic</s:NameID>`} {
		blob, _ := json.Marshal(map[string]string{"tenant": "mailto:issuer@example.com?list[]=one two", "subject": subject})
		canonical, err := decodeUserMigrationSSO(string(blob))
		if err != nil {
			t.Fatal(err)
		}
		again, err := decodeUserMigrationSSO(string(canonical))
		if err != nil || string(again) != string(canonical) {
			t.Fatal("SAML storage is not idempotent")
		}
		var result map[string]string
		_ = json.Unmarshal(canonical, &result)
		if !strings.Contains(result["tenant"], "mailto:issuer@example.com?list%5B%5D=one%20two") {
			t.Fatal("native absolute scheme/lax query canonicalization changed")
		}
	}
}

func TestUserSSOMigrationPreservesNativeEntityQualifierQuirkAndXMLLimits(t *testing.T) {
	issuer := `<Issuer xmlns="urn:oasis:names:tc:SAML:2.0:assertion" NameQualifier="one" SPNameQualifier="two"> http://example.com/ </Issuer>`
	canonical, err := storedSAMLIssuer(issuer)
	if err != nil || strings.Contains(canonical, "Qualifier") || !strings.Contains(canonical, ">http://example.com/</Issuer>") {
		t.Fatal("native entity qualifier discard/URI trim changed")
	}
	allThree := strings.Replace(issuer, `SPNameQualifier="two"`, `SPNameQualifier="two" SPProvidedID="three"`, 1)
	if _, err := storedSAMLIssuer(allThree); err == nil {
		t.Fatal("native three-qualifier entity rejection missing")
	}
	for _, format := range []string{"persistent", "transient"} {
		value := `<NameID xmlns="urn:oasis:names:tc:SAML:2.0:assertion" Format="` + samlNameFormats[format] + `">` + strings.Repeat("x", 1025) + `</NameID>`
		_, err := storedSAMLSubject(value)
		if (err != nil) != (format == "persistent") {
			t.Fatal("native persistent-only length limit changed")
		}
	}
}

func TestUserSSOMigrationRejectsTypedAndMixedIdentitiesWithoutRawDiagnostics(t *testing.T) {
	for _, value := range []any{[]byte("private"), "null", `[]`, `{"tenant":"http://example.com/"}`, `{"scim_external_id":123}`, `{"tenant":"http://example.com/","subject":"synthetic","scim_external_id":"private"}`, `{"tenant":"relative","subject":"synthetic"}`} {
		if _, err := decodeUserMigrationSSO(value); err == nil || strings.Contains(err.Error(), "private") {
			t.Fatal("invalid SSO value accepted or raw value exposed")
		}
	}
	// The parser never resolves file/network entities. Native plaintext fallback
	// still accepts short malformed XML as a Name, so this payload exceeds128.
	payload := `<!DOCTYPE NameID [<!ENTITY x SYSTEM "file:///synthetic-private">]><NameID xmlns="urn:oasis:names:tc:SAML:2.0:assertion">&x;</NameID>`
	if _, err := storedSAMLSubject(payload); err == nil {
		t.Fatal("external entity payload accepted")
	}
	if _, err := decodeUserMigrationSSO(`{"tenant":null,"subject":null,"scim_external_id":""}`); err != nil {
		t.Fatal("native nullable optional keys/empty SCIM rejected")
	}
}
