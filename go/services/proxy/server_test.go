// SPDX-License-Identifier: AGPL-3.0-or-later
package proxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

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
