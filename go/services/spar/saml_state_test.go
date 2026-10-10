// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
)

func TestNativeTTLConfigRoundingAndBounds(t *testing.T) {
	for _, c := range []struct {
		input string
		want  TTL
	}{
		{"1.5", 2}, {"2.5", 2}, {"-1.5", -2}, {"-2.5", -2}, {"2147483648", -2147483648},
		{"4294967297.5", 2}, {"9007199254740993", 1}, {"1.01e2", 101}, {"-4294967297", -1},
	} {
		var ttl TTL
		if err := yaml.Unmarshal([]byte(c.input), &ttl); err != nil || ttl != c.want {
			t.Fatalf("%s: %d, %v", c.input, ttl, err)
		}
	}
	for _, bad := range []string{`"60"`, ".inf", ".nan", "[60]", "true"} {
		var ttl TTL
		if yaml.Unmarshal([]byte(bad), &ttl) == nil {
			t.Fatalf("invalid TTL accepted: %s", bad)
		}
	}
	now := time.Unix(1700000000, 0)
	if ttl, err := checkedTTL("authreq", 2, now, now.Add(2500*time.Millisecond)); err != nil || ttl != 2 {
		t.Fatal(ttl, err)
	}
	for _, c := range []struct {
		max     TTL
		seconds float64
		message string
	}{
		{10, .5, `TTLNegative "TTL:authreq:0"`},
		{10, 10.6, `TTLTooLong "TTL:authreq:11" "TTL:authreq:10"`},
		{-2, -1, `TTLTooLong "TTL:authreq:-1" "TTL:authreq:-2"`},
		{10, 2147483648, `TTLNegative "TTL:authreq:-2147483648"`},
	} {
		_, err := checkedTTL("authreq", c.max, now, now.Add(time.Duration(c.seconds*1e9)))
		if err == nil || err.Error() != c.message {
			t.Fatal(c, err)
		}
		w := httptest.NewRecorder()
		domainError(w, err)
		if w.Code != 400 || !strings.Contains(w.Body.String(), `"label":"ttl-error"`) {
			t.Fatal("TTL error contract", w)
		}
	}
}

func TestStoredVerdictRejectsInvalidAndIncompleteRows(t *testing.T) {
	web, mobile, unknown := int32(0), int32(1), int32(2)
	success, failure, label := "wire://success/$userid", "wire://error/$label", "synthetic-device"
	for _, c := range []struct {
		con              *int32
		success, failure *string
		exists, failed   bool
	}{
		{&web, nil, nil, true, false}, {&mobile, &success, &failure, true, false},
		{&web, &success, nil, false, false}, {&mobile, &success, nil, false, false},
		{&unknown, nil, nil, false, true}, {nil, nil, nil, false, true},
	} {
		format, err := storedVerdict(c.con, c.success, c.failure, &label)
		if (format != nil) != c.exists || (err != nil) != c.failed {
			t.Fatal(c, format, err)
		}
		if format != nil && *format.Label != label {
			t.Fatal("cookie label lost")
		}
	}
	bad := "relative-uri"
	if _, err := storedVerdict(&mobile, &bad, &failure, nil); err == nil {
		t.Fatal("invalid stored URI accepted")
	}
}

func TestOriginalNameIDGoldenAndUnicodeLookupKeys(t *testing.T) {
	encoded, err := os.ReadFile("../../../libs/wire-api/test/golden/testObject_UserSSOId_user_2.json")
	if err != nil {
		t.Fatal(err)
	}
	var original struct {
		Subject string `json:"subject"`
	}
	if err = json.Unmarshal(encoded, &original); err != nil {
		t.Fatal(err)
	}
	id, err := parseNameID(original.Subject)
	if err != nil || id.encoded() != original.Subject {
		t.Fatal("original legacy XML key changed", id.encoded(), err)
	}
	for _, c := range []struct{ value, normalized string }{{"Straße", "strasse"}, {"Σςσ", "σσσ"}, {"É", "é"}, {"e\u0301", "e\u0301"}} {
		id.Value = c.value
		id.Format = nameID11 + "unspecified"
		qualifier := "different-team.example"
		id.NameQualifier = &qualifier
		if id.normalized() != c.normalized {
			t.Fatal("lookup key changed", c)
		}
	}
	// Preserve quotes and XML escaping through the canonical legacy key; XML
	// prefixes are parsed by expanded name, never used as a trust shortcut.
	id.Value = `a<&>"'b`
	parsed, err := parseNameID(id.encoded())
	if err != nil || parsed.Value != id.Value || parsed.encoded() != id.encoded() {
		t.Fatal("qualified XML round trip", err)
	}
	for _, bad := range []string{
		`<NameID xmlns="wrong">user</NameID>`,
		`<NameID xmlns="` + assertionNS + `" Format="unsupported">user</NameID>`,
		`<NameID xmlns="` + assertionNS + `"><NameID>injected</NameID></NameID>`,
		`<!DOCTYPE NameID [<!ENTITY leak SYSTEM "file:///synthetic">]><NameID xmlns="` + assertionNS + `">&leak;</NameID>`,
		original.Subject + original.Subject,
	} {
		if _, err := parseNameID(bad); err == nil {
			t.Fatal("invalid NameID accepted")
		}
	}
}
