package cargohold

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type versionTransport func(*http.Request) (*http.Response, error)

func (f versionTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestFederationVersionNegotiation(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		version    int
		failure    bool
	}{
		{"newest-common", `{"supported":[0,1],"supported_versions":[0,2,4,9]}`, 4, false},
		{"legacy", `{"supported":[0,1]}`, 0, false},
		{"invalid-type", `{"supported":[0,1],"supported_versions":"invalid"}`, 0, true},
		{"no-common", `{"supported":[0,1],"supported_versions":[9]}`, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := Config{}
			c.Settings.FederationDomain = "example.com"
			c.Federator = &Endpoint{Host: "127.0.0.1", Port: 8097}
			s := NewServer(c, &fixtureStore{})
			s.federationClient = &http.Client{Transport: versionTransport(func(r *http.Request) (*http.Response, error) {
				body, _ := io.ReadAll(r.Body)
				if r.Method != "POST" || r.URL.Path != "/rpc/foreign.example.com/brig/api-version" || string(body) != "[]" || r.Header.Get("Wire-Origin-Domain") != "example.com" || r.Header.Get("Wire-Origin-Request-Id") != "synthetic-request" {
					t.Fatal("incorrect negotiation wire request")
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})}
			r := httptest.NewRequest("GET", "/", nil)
			r.Header.Set("Request-Id", "synthetic-request")
			v, err := s.negotiateFederation(r, "foreign.example.com")
			if (err != nil) != tc.failure || (!tc.failure && v != tc.version) {
				t.Fatalf("version=%d error=%v", v, err)
			}
		})
	}
}
