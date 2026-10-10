// SPDX-License-Identifier: AGPL-3.0-only
package brig

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

func wireError(w http.ResponseWriter, code int, label, message string) {
	w.Header().Set("Content-Type", "application/json;charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "label": label, "message": message})
}

func disabledVersion(c Config, v int) bool {
	for _, expression := range c.Settings.DisabledVersions {
		if expression == "v"+strconv.Itoa(v) || expression == "development" && v == 19 {
			return true
		}
	}
	return false
}

// This initial handler implements liveness/version and version middleware.
// Business API families and documentation are still under construction. No
// successful fallback response substitutes for a missing business handler.
func NewHandler(c Config) http.Handler {
	return newHandler(c, nil)
}

func newHandler(c Config, business *brigReadHandlers) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.Clone(r.Context())
		r.Header.Del("X-Wire-API-Version")
		path := r.URL.Path
		segments := strings.Split(strings.TrimPrefix(path, "/"), "/")
		first := segments[0]
		looksVersion := strings.HasPrefix(first, "v")
		for _, ch := range strings.TrimPrefix(first, "v") {
			if ch < '0' || ch > '9' {
				looksVersion = false
			}
		}
		if looksVersion {
			if len(segments) > 1 && (segments[1] == "i" || segments[1] == "api-internal") {
				wireError(w, 404, "unsupported-version", "Internal APIs (`/i/...`) are not under version control")
				return
			}
			v, err := strconv.Atoi(first[1:])
			path = "/" + strings.Join(segments[1:], "/")
			isDocs := strings.HasPrefix(path, "/api/swagger-ui/") || path == "/api/swagger.json" || strings.HasPrefix(path, "/api/swagger.json/")
			if err != nil || first != "v"+strconv.Itoa(v) || v < 0 || v > 19 || disabledVersion(c, v) && !isDocs {
				wireError(w, 404, "unsupported-version", "Version "+first+" is not supported")
				return
			}
			r.Header.Set("X-Wire-API-Version", strconv.Itoa(v))
		}
		if strings.HasPrefix(path, "/i/users/handles") && (path == "/i/users/handles" || strings.HasPrefix(path, "/i/users/handles/")) {
			path = "/i/handles" + strings.TrimPrefix(path, "/i/users/handles")
		}
		if _, present := r.Header["Request-Id"]; !present {
			var raw [16]byte
			if _, err := rand.Read(raw[:]); err != nil {
				wireError(w, 500, "server-error", "Internal Server Error")
				return
			}
			raw[6] = (raw[6] & 15) | 64
			raw[8] = (raw[8] & 63) | 128
			id := hex.EncodeToString(raw[:])
			r.Header.Set("Request-Id", id[:8]+"-"+id[8:12]+"-"+id[12:16]+"-"+id[16:20]+"-"+id[20:])
		}
		switch path {
		case "/i/status":
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				w.WriteHeader(405)
				return
			}
			w.Header().Set("Content-Type", "application/json;charset=utf-8")
			w.WriteHeader(200)
		case "/api-version":
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				w.WriteHeader(405)
				return
			}
			supported := []int{}
			development := []int{}
			for v := 0; v <= 19; v++ {
				if disabledVersion(c, v) {
					continue
				}
				if v == 19 {
					development = append(development, v)
				} else {
					supported = append(supported, v)
				}
			}
			w.Header().Set("Content-Type", "application/json;charset=utf-8")
			if r.Method == http.MethodHead {
				w.WriteHeader(200)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"supported": supported, "development": development, "federation": c.Federator != nil, "domain": c.Settings.Domain})
		default:
			if business != nil && business.handle(w, r, path) {
				return
			}
			w.WriteHeader(404)
		}
	})
}

// Run currently supplies the first runnable startup slice on initialized
// native Cassandra schemas, applying native-compatible PostgreSQL migrations.
// Dependency/background initialization and business handlers remain required
// before this replacement can qualify.
func Run(ctx context.Context, c Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if _, _, err := c.signingKeys(); err != nil {
		return err
	}
	cassandra, err := openCassandra(ctx, c)
	if err != nil {
		return err
	}
	defer cassandra.Close()
	postgres, err := openPostgres(ctx, c)
	if err != nil {
		return err
	}
	defer postgres.Close()
	if err = runPostgresMigrations(ctx, postgres, time.Duration(c.PostgresPool.Acquisition)); err != nil {
		return err
	}
	listener, err := net.Listen("tcp", c.Brig.Address())
	if err != nil {
		return errors.New("Brig listener initialization failed")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	defer transport.CloseIdleConnections()
	business := brigReadHandlers{config: c, users: userStore{userAuthStore{cassandra: cassandra, postgres: postgres, mode: c.Migration["user"], acquisition: time.Duration(c.PostgresPool.Acquisition)}}, sessions: sessionStore{cassandra}, peers: brigPeers{client: &http.Client{Transport: transport, Timeout: 30 * time.Second}, galley: c.Galley}}
	server := &http.Server{Handler: newHandler(c, &business), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	return serve(ctx, server, listener)
}

func serve(ctx context.Context, server *http.Server, listener net.Listener) error {
	result := make(chan error, 1)
	go func() { result <- server.Serve(listener) }()
	select {
	case err := <-result:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return errors.New("Brig HTTP server failed")
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
			return errors.New("Brig shutdown drain failed")
		}
		if err := <-result; !errors.Is(err, http.ErrServerClosed) {
			return errors.New("Brig HTTP server failed")
		}
		return nil
	}
}
