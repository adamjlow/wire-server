// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func wireError(w http.ResponseWriter, status int, label, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"code": status, "label": label, "message": message})
}
func decimal(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
func NewHandler(c Config) http.Handler { return newHandler(c, nil) }

type sparHTTP struct {
	http.Handler
	metrics *httpMetrics
}

type metricWriter struct {
	http.ResponseWriter
	status int
}

func (w *metricWriter) WriteHeader(code int) {
	if code >= 100 && code < 200 {
		w.ResponseWriter.WriteHeader(code)
		return
	}
	if w.status != 0 {
		return
	}
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}
func (w *metricWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}
func (w *metricWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func newHandler(c Config, store settingsStore) *sparHTTP {
	metrics := newHTTPMetrics()
	peers := peerClient{config: c, http: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	return &sparHTTP{metrics: metrics, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		segments := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
		first := segments[0]
		if strings.HasPrefix(first, "v") && decimal(first[1:]) {
			if len(segments) > 1 && (segments[1] == "i" || segments[1] == "api-internal") {
				wireError(w, 404, "unsupported-version", "Internal APIs (`/i/...`) are not under version control")
				return
			}
			disabled := false
			for _, v := range c.DisabledVersions {
				if v == first || (v == "development" && first == "v19") {
					disabled = true
				}
			}
			if !knownVersion(first) || disabled {
				wireError(w, 404, "unsupported-version", "Version "+first+" is not supported")
				return
			}
			r = r.Clone(r.Context())
			r.URL.Path = "/" + strings.Join(segments[1:], "/")
			r.Header.Del("X-Wire-API-Version")
			r.Header.Set("X-Wire-API-Version", strings.TrimPrefix(first, "v"))
		}
		requestID := r.Header.Get("Request-Id")
		if _, present := r.Header["Request-Id"]; !present {
			var raw [16]byte
			if _, err := rand.Read(raw[:]); err == nil {
				raw[6] = (raw[6] & 15) | 64
				raw[8] = (raw[8] & 63) | 128
				encoded := hex.EncodeToString(raw[:])
				requestID = encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:]
			}
		}
		r.Header.Set("Request-Id", requestID)
		observed := &metricWriter{ResponseWriter: w}
		w = observed
		start := time.Now()
		defer func() {
			status := observed.status
			if status == 0 {
				status = http.StatusOK
			}
			metrics.observe(r.URL.Path, r.Method, status, time.Since(start))
		}()
		// Prometheus intercepts the route before gunzip/cache policy, as in WAI.
		if r.URL.Path == "/i/metrics" && r.Method == http.MethodGet {
			metrics.serve(w)
			return
		}
		if r.Header.Get("Content-Encoding") == "gzip" {
			r = r.Clone(r.Context())
			r.Header.Del("Content-Encoding")
			r.Header.Del("Content-Length")
			r.ContentLength = -1
			body := &gunzipBody{source: r.Body}
			r.Body = body
			w = &gunzipWriter{ResponseWriter: w, body: body}
			defer r.Body.Close()
		}
		w.Header().Set("Cache-Control", "no-cache, no-store")
		w.Header().Set("Pragma", "no-cache")
		w.Header()["Content-Type"] = nil
		if r.URL.Path == "/i/status" {
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("Content-Type", "application/json;charset=utf-8")
			w.WriteHeader(http.StatusOK)
			return
		}
		if serveSCIMMetadata(w, r) {
			return
		}
		if cleanup, ok := store.(internalCleanupStore); ok && serveInternalCleanup(w, r, cleanup, peers) {
			return
		}
		if discovery, ok := store.(emailDiscoveryStore); ok && serveEmailDiscovery(w, r, c, discovery, peers) {
			return
		}
		if db, ok := store.(cassandraStore); ok && serveIdPs(w, r, c, db, peers) {
			return
		}
		if saml, ok := store.(samlRequestStore); ok && serveSAMLRequest(w, r, c, saml) {
			return
		}
		if saml, ok := store.(samlFinalizeStore); ok && serveSAMLFinalize(w, r, c, saml, peers) {
			return
		}
		if tokens, ok := store.(tokenStore); ok && serveTokens(w, r, c, tokens, peers) {
			return
		}
		if tokens, ok := store.(scimTokenLookup); ok && serveSCIMGroups(w, r, c, tokens, peers) {
			return
		}
		if users, ok := store.(scimUserStore); ok && serveSCIMUsers(w, r, c, users, peers) {
			return
		}
		if store != nil && serveSettings(w, r, store) {
			return
		}
		// Unknown route.
		w.WriteHeader(http.StatusNotFound)
	})}
}
func Run(ctx context.Context, c Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	session, err := openStore(ctx, c)
	if err != nil {
		return err
	}
	defer session.Close()
	listener, err := net.Listen("tcp", net.JoinHostPort(c.SAML.Host, strconv.Itoa(c.SAML.Port)))
	if err != nil {
		return errors.New("cannot bind Spar HTTP listener")
	}
	handler := newHandler(c, cassandraStore{session: session})
	metricsCtx, stopMetrics := context.WithCancel(ctx)
	defer stopMetrics()
	go handler.metrics.Collect(metricsCtx)
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 30 * time.Second, ConnState: handler.metrics.ConnState}
	stopped := make(chan error, 1)
	go func() { stopped <- server.Serve(listener) }()
	select {
	case err = <-stopped:
		if !errors.Is(err, http.ErrServerClosed) {
			return errors.New("Spar HTTP server failed")
		}
		return nil
	case <-ctx.Done():
		drain, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if server.Shutdown(drain) != nil {
			server.Close()
			return errors.New("Spar shutdown timed out")
		}
		<-stopped
		return nil
	}
}
