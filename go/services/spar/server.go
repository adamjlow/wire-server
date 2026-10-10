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

func newHandler(c Config, store settingsStore) http.Handler {
	peers := peerClient{config: c, http: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		if tokens, ok := store.(tokenStore); ok && serveTokens(w, r, c, tokens, peers) {
			return
		}
		if store != nil && serveSettings(w, r, store) {
			return
		}
		// Remaining source routes
		// are not successful placeholders and must be implemented before selection.
		w.WriteHeader(http.StatusNotFound)
	})
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
	server := &http.Server{Handler: newHandler(c, cassandraStore{session: session}), ReadHeaderTimeout: 30 * time.Second}
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
