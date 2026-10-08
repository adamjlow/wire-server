package cannon

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAckStrictTypesAndRange(t *testing.T) {
	for _, raw := range []string{`{`, `{}`, `{"type":"ack","data":{"delivery_tag":1}}`, `{"type":"ack","data":{"delivery_tag":null,"multiple":false}}`, `{"type":"ack","data":{"delivery_tag":-1,"multiple":false}}`, `{"type":"ack","data":{"delivery_tag":1.5,"multiple":false}}`, `{"type":"ack","data":{"delivery_tag":18446744073709551616,"multiple":false}}`, `{"type":"ack","data":{"delivery_tag":1,"multiple":"false"}}`} {
		if _, err := parseAck([]byte(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	for _, raw := range []string{`{"type":"ack","data":{"delivery_tag":18446744073709551615,"multiple":false}}`, `{"type":"ack","data":{"delivery_tag":0,"multiple":true}}`, `{"type":"ack_full_sync"}`} {
		if _, err := parseAck([]byte(raw)); err != nil {
			t.Fatalf("rejected %s: %v", raw, err)
		}
	}
}
func TestTLSRequiresTrustedCertificate(t *testing.T) {
	provider := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer provider.Close()
	config, err := verifiedTLS("127.0.0.1", "")
	if err != nil {
		t.Fatal(err)
	}
	if config.InsecureSkipVerify || config.MinVersion < tls.VersionTLS12 {
		t.Fatal("verification disabled")
	}
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: config}}
	if response, err := client.Get(provider.URL); err == nil {
		response.Body.Close()
		t.Fatal("untrusted certificate accepted")
	}
}

func TestWord64PreservesIntegralJSONNotationAndBounds(t *testing.T) {
	for raw, want := range map[string]uint64{
		"1.0": 1, "1e3": 1000, "1000e-3": 1, "-0.0": 0,
		"0e1000000": 0, "18446744073709551615.0": ^uint64(0),
		"1.8446744073709551615e19": ^uint64(0),
	} {
		got, err := decodeWord64([]byte(raw))
		if err != nil || got != want {
			t.Fatalf("%s: got %d %v want %d", raw, got, err, want)
		}
	}
	for _, raw := range []string{"null", `"1"`, "[]", "true", "1.01", "-1.0", "18446744073709551616.0", "1e1000000", "1e-1000000", "1e9223372036854775807", "1e-9223372036854775808"} {
		if _, err := decodeWord64([]byte(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	ack, err := parseAck([]byte(`{"type":"ack","data":{"delivery_tag":0.0,"multiple":true}}`))
	if err != nil || ack.Tag != 0 || !ack.Multiple {
		t.Fatalf("integral acknowledgement: %+v %v", ack, err)
	}
}
