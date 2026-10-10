// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInternalHTTPProxySourceRules(t *testing.T) {
	for _, test := range []struct {
		name          string
		env           []string
		target, proxy string
	}{
		{"bare-localhost", []string{"HTTP_PROXY=proxy.example:8080"}, "http://localhost/i/status", "http://proxy.example:8080"},
		{"lowercase-empty-wins", []string{"HTTP_PROXY=http://proxy.example", "http_proxy="}, "http://service.example/", ""},
		{"domain-suffix", []string{"http_proxy=http://proxy.example/", "no_proxy= EXAMPLE.COM"}, "http://sub.example.com/", ""},
		{"domain-boundary", []string{"http_proxy=http://proxy.example", "no_proxy=example.com"}, "http://otherexample.com/", "http://proxy.example"},
		{"literal-wildcard", []string{"http_proxy=http://proxy.example", "no_proxy=*"}, "http://service.example/", "http://proxy.example"},
		{"literal-port", []string{"http_proxy=http://proxy.example", "no_proxy=service.example:80"}, "http://service.example/", "http://proxy.example"},
		{"trailing-space-retained", []string{"http_proxy=http://proxy.example", "no_proxy=service.example "}, "http://service.example/", "http://proxy.example"},
		{"scheme-selection", []string{"http_proxy=http://one.example", "https_proxy=http://two.example"}, "https://service.example/", "http://two.example"},
	} {
		t.Run(test.name, func(t *testing.T) {
			selectProxy, err := internalHTTPProxy(test.env)
			if err != nil {
				t.Fatal(err)
			}
			request, _ := http.NewRequest("GET", test.target, nil)
			proxy, err := selectProxy(request)
			actual := ""
			if proxy != nil {
				actual = proxy.String()
			}
			if err != nil || actual != test.proxy {
				t.Fatalf("proxy=%q, error=%v", actual, err)
			}
		})
	}
	for _, invalid := range []string{"https://proxy.example", "http://proxy.example:", "http://proxy.example/path", "http://proxy.example?secret", "http://proxy.example#secret", "http://secret:password@proxy.example:bad"} {
		if _, err := internalHTTPProxy([]string{"http_proxy=" + invalid}); err == nil || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "password") {
			t.Fatal("invalid proxy accepted or error disclosed credentials")
		}
	}
}

func TestInternalHTTPProxyActualRoutingAuthAndSnapshot(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.String() != "http://localhost/i/status" || r.Header.Get("Proxy-Authorization") != "Basic dXNlcjpzeW50aGV0aWM=" {
			t.Error("proxy destination or synthetic authentication changed")
		}
		_, _ = io.WriteString(w, "proxy-ok")
	}))
	defer proxy.Close()
	t.Setenv("http_proxy", strings.Replace(proxy.URL, "http://", "http://user:synthetic@", 1))
	t.Setenv("https_proxy", "")
	t.Setenv("no_proxy", "")
	transport, err := newInternalHTTPTransport()
	if err != nil {
		t.Fatal(err)
	}
	defer transport.CloseIdleConnections()
	t.Setenv("http_proxy", "")
	t.Setenv("no_proxy", "localhost")
	request, _ := http.NewRequestWithContext(t.Context(), "GET", "http://localhost/i/status", nil)
	response, err := (&http.Client{Transport: transport}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || string(body) != "proxy-ok" {
		t.Fatal("snapshot proxy routing failed")
	}
}
