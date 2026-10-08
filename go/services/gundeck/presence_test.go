package gundeck

import "testing"

func TestLegacyPresencePreservesOriginalField(t *testing.T) {
	p, ok := decodePresence("synthetic-user", "connection@old-cannon", "http://localhost:8083/i/push/synthetic-user/connection")
	if !ok || p.Device != "connection" || p.field != "connection@old-cannon" || p.Client != nil || p.Created != 0 {
		t.Fatalf("legacy presence lost: %#v %v", p, ok)
	}
	for _, bad := range []string{`{`, `{"r":"http://localhost:8083","c":123}`, `{"r":"http://localhost:8083","t":"bad"}`, `{"r":"not-an-absolute-uri"}`} {
		if _, ok := decodePresence("synthetic-user", "connection", bad); ok {
			t.Fatalf("accepted malformed stored presence %q", bad)
		}
	}
}
func TestIdentifiersRejectMalformedValues(t *testing.T) {
	for _, v := range []string{"", "invalid", "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaa!"} {
		if uuidValid(v) {
			t.Fatalf("accepted malformed UUID %q", v)
		}
	}
	for _, v := range []string{"", "client", "ab cd", "abcd/other"} {
		if clientValid(v) {
			t.Fatalf("accepted malformed client %q", v)
		}
	}
	if !uuidValid("44444444-4444-4444-8444-444444444444") || !clientValid("abcd") {
		t.Fatal("valid fixture principals rejected")
	}
}

func TestClientIDsRespectWord64AndCanonicalRendering(t *testing.T) {
	for _, client := range []string{"10000000000000000", "fffffffffffffffff", "-1", ""} {
		if clientValid(client) {
			t.Fatalf("accepted invalid client %q", client)
		}
	}
	for input, expected := range map[string]string{"000ABCD": "abcd", "000": "0", "FFFFFFFFFFFFFFFF": "ffffffffffffffff"} {
		if !clientValid(input) || canonicalClient(input) != expected {
			t.Fatalf("client %q changed canonical representation", input)
		}
	}
}
