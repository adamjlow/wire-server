// SPDX-License-Identifier: AGPL-3.0-or-later
package proxy

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

func TestProductionTransportUsesHTTP1WithHTTP2CapableTLSProvider(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		if r.Proto != "HTTP/1.1" || r.TLS == nil || r.TLS.NegotiatedProtocol == "h2" {
			t.Errorf("provider protocol differs from reference: %s", r.Proto)
		}
		_, _ = w.Write([]byte("synthetic"))
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	t.Cleanup(server.Close)
	u, _ := url.Parse(server.URL)
	port, _ := strconv.Atoi(u.Port())
	h := NewHandler(Config{GiphyEndpoint: &Endpoint{Host: u.Hostname(), Port: port}, HTTPPoolSize: 10},
		&Secrets{values: map[string]any{"secrets.giphy": "synthetic-giphy-secret"}}).(*handler)
	transport := h.client.Transport.(*http.Transport)
	pool := x509.NewCertPool()
	pool.AddCert(server.Certificate())
	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = new(tls.Config)
	}
	transport.TLSClientConfig.RootCAs = pool
	t.Cleanup(transport.CloseIdleConnections)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/proxy/giphy/v1/gifs/search", nil))
	if w.Code != 200 || w.Body.String() != "synthetic" || attempts.Load() != 1 {
		t.Fatalf("production TLS transport failed: status=%d attempts=%d", w.Code, attempts.Load())
	}
}

func providerHandler(t *testing.T, provider http.HandlerFunc) (*handler, string) {
	t.Helper()
	server := httptest.NewTLSServer(provider)
	t.Cleanup(server.Close)
	u, _ := url.Parse(server.URL)
	port, _ := strconv.Atoi(u.Port())
	endpoint := &Endpoint{Host: u.Hostname(), Port: port}
	cfg := Config{GiphyEndpoint: endpoint, YoutubeEndpoint: endpoint, GoogleMapsEndpoint: endpoint,
		SpotifyEndpoint: endpoint, SoundcloudEndpoint: endpoint}
	secrets := &Secrets{values: map[string]any{"secrets.giphy": "synthetic-giphy-secret", "secrets.youtube": "synthetic-youtube-secret",
		"secrets.googlemaps": "synthetic-googlemaps-secret", "secrets.spotify": "synthetic-spotify-secret", "secrets.soundcloud": "synthetic-soundcloud-secret"}}
	return newHandler(cfg, secrets, server.Client().Transport), server.URL
}

func TestAllProviderFamiliesAgainstObservedExpectations(t *testing.T) {
	for _, test := range []struct{ target, outgoing string }{
		{"/proxy/giphy/v1/gifs/search?q=one+two&ACCESS_TOKEN=discard", "/v1/gifs/search?api_key=synthetic-giphy-secret&q=one%20two"},
		{"/proxy/youtube/v3/search", "/youtube/v3/search?key=synthetic-youtube-secret"},
		{"/proxy/googlemaps/api/staticmap", "/maps/api/staticmap?key=synthetic-googlemaps-secret"},
		{"/proxy/googlemaps/maps/api/geocode/search", "/maps/api/geocode/search?key=synthetic-googlemaps-secret"},
		{"/proxy/spotify/api/token", "/api/token"},
		{"/proxy/soundcloud/resolve?url=https%3A%2F%2Fsynthetic.invalid%2Fsong", "/resolve?url=https%3A%2F%2Fsynthetic.invalid%2Fsong&client_id=synthetic-soundcloud-secret"},
	} {
		t.Run(test.target, func(t *testing.T) {
			var attempts atomic.Int32
			h, _ := providerHandler(t, func(w http.ResponseWriter, r *http.Request) {
				attempts.Add(1)
				if r.RequestURI != test.outgoing {
					t.Errorf("outgoing target %q, want %q", r.RequestURI, test.outgoing)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"provider":"synthetic"}`))
			})
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("GET", test.target, nil))
			if w.Code != 200 || attempts.Load() != 1 || w.Body.String() != `{"provider":"synthetic"}` {
				t.Fatal("provider response/effects differ")
			}
			if strings.Contains(test.target, "spotify") || strings.Contains(test.target, "resolve") {
				values := w.Header().Values("Content-Type")
				if len(values) != 2 || values[0] != "application/json" || values[1] != "text/plain; charset=UTF-8" {
					t.Fatal("observed duplicate content types lost")
				}
			}
		})
	}
}

func TestSpotifyCredentialBoundaryAndIncomingPost(t *testing.T) {
	h, _ := providerHandler(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Method != "POST" || string(body) != "synthetic=body" || r.Header.Get("Authorization") != "synthetic-spotify-secret" ||
			r.Header.Get("X-Client") != "" || r.Header.Get("Accept") != "application/json" {
			t.Error("incoming credentials/body/headers crossed boundary incorrectly")
		}
		w.WriteHeader(200)
	})
	r := httptest.NewRequest("POST", "/proxy/spotify/api/token", strings.NewReader("synthetic=body"))
	r.Header.Set("Authorization", "synthetic-client-secret")
	r.Header.Set("X-Client", "must-not-forward")
	r.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal("Spotify inbound POST rejected")
	}
}

func TestRawRetryAndWrongMethod(t *testing.T) {
	var attempts atomic.Int32
	h, _ := providerHandler(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "gEt" && r.Header.Get("Content-Length") != "0" {
			t.Error("mixed-case GET lost source's explicit zero content length")
		}
		if attempts.Add(1) < 3 {
			w.WriteHeader(502)
			return
		}
		w.WriteHeader(200)
	})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("gEt", "/proxy/giphy/v1/gifs/search", nil))
	if w.Code != 200 || attempts.Load() != 3 {
		t.Fatal("source retry/mixed-case method behavior changed")
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/proxy/giphy/v1/gifs/search", nil))
	if w.Code != 405 || attempts.Load() != 3 || w.Body.String() != `{"code":405,"label":"method-not-allowed","message":"Method not allowed"}` {
		t.Fatal("method error/effects changed")
	}
}

func TestFinalUpstreamErrorsUseSharedErrorContract(t *testing.T) {
	for _, test := range []struct {
		status         int
		body, expected string
	}{
		{502, `{"provider":"synthetic"}`, `{"code":502,"label":"server-error","message":"{\"provider\":\"synthetic\"}"}`},
		{500, "synthetic-sensitive-detail", `{"code":500,"label":"server-error","message":"Internal Server Error"}`},
		{503, `{"code":503,"label":"custom","message":"synthetic message"}`, `{"code":503,"label":"custom","message":"synthetic message"}`},
		{503, `{"code":503,"label":"custom","message":"<synthetic>&","data":{"domain":"EXAMPLE.COM","path":"/synthetic"},"inner":{"code":500,"label":"inner","message":"detail"}}`, `{"code":503,"data":{"domain":"example.com","path":"/synthetic","type":"federation"},"inner":{"code":500,"label":"inner","message":"detail"},"label":"custom","message":"<synthetic>&"}`},
	} {
		for _, path := range []string{"/proxy/giphy/v1/gifs/search", "/proxy/spotify/api/token"} {
			var attempts atomic.Int32
			h, _ := providerHandler(t, func(w http.ResponseWriter, r *http.Request) {
				attempts.Add(1)
				w.Header().Set("X-Provider", "must-not-forward")
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			})
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
			wantAttempts := int32(1)
			if test.status == 502 && strings.Contains(path, "giphy") {
				wantAttempts = 3
			}
			if w.Code != test.status || w.Body.String() != test.expected || w.Header().Get("X-Provider") != "" || attempts.Load() != wantAttempts {
				t.Fatalf("%s status %d: response/effects differ: %d %s", path, test.status, w.Code, w.Body.String())
			}
		}
	}
}

func TestStreamRedirectAndHostSchemeGuards(t *testing.T) {
	var attempts atomic.Int32
	h, provider := providerHandler(t, func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.Header().Set("Location", "https://synthetic.invalid/download")
		w.WriteHeader(302)
	})
	for _, method := range []string{"GET", "POST"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(method, "/proxy/soundcloud/stream?url="+url.QueryEscape(provider+"/audio"), nil))
		if w.Code != 302 || w.Body.Len() != 0 || w.Header().Get("Location") != "https://synthetic.invalid/download" {
			t.Fatal("stream did not preserve source's empty redirect")
		}
	}
	for _, target := range []string{"https://other.invalid/audio", strings.Replace(provider, "https:", "http:", 1) + "/audio"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/proxy/soundcloud/stream?url="+url.QueryEscape(target), nil))
		if w.Code != 500 || attempts.Load() != 2 || strings.Contains(w.Body.String(), "synthetic-soundcloud-secret") {
			t.Fatal("stream guard leaked credentials or made a disallowed request")
		}
	}
}

func TestVersionsEndpathHealthAndMissingURL(t *testing.T) {
	h := newHandler(Config{DisabledAPIVersions: []string{"development", "v0"}}, &Secrets{}, http.DefaultTransport)
	for _, test := range []struct {
		path   string
		status int
	}{
		{"/i/status", 200}, {"/unknown", 404}, {"/v0/i/status", 404}, {"/v99/proxy/giphy/v1/gifs", 404},
		{"/v19/proxy/giphy/v1/gifs", 404}, {"/v0/proxy/giphy/v1/gifs", 404},
		{"/proxy/googlemaps/api/staticmap/extra", 404}, {"/proxy/spotify/api/token/extra", 404},
		{"/proxy/soundcloud/resolve", 400}, {"/proxy/soundcloud/stream", 400},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", test.path, nil))
		if w.Code != test.status {
			t.Fatalf("%s returned %d, want %d", test.path, w.Code, test.status)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if pause(ctx, 5000000000) {
		t.Fatal("retry ignored cancellation")
	}
}
