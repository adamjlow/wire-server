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
