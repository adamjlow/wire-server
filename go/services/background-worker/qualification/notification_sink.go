// SPDX-License-Identifier: AGPL-3.0-only
// Qualification-only h2c sink: holds actual delivery until cancellation.
package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

func main() {
	var entered atomic.Int64
	var accepted atomic.Bool
	var mu sync.Mutex
	var deliveries []map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/qualification/status", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"entered": entered.Load(), "deliveries": deliveries})
	})
	mux.HandleFunc("/qualification/accept", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", 405)
			return
		}
		accepted.Store(true)
		w.WriteHeader(204)
	})
	mux.HandleFunc("/rpc/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", 405)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 65536)
		if strings.HasSuffix(r.URL.Path, "/api-version") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"supported":[0],"supported_versions":[0]}`))
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "body", 400)
			return
		}
		entered.Add(1)
		mu.Lock()
		deliveries = append(deliveries, map[string]any{"path": r.URL.Path, "body": string(body), "accepted": accepted.Load()})
		mu.Unlock()
		if accepted.Load() {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("{}"))
			return
		}
		<-r.Context().Done()
	})
	log.Fatal(http.ListenAndServe("127.0.0.1:16197", h2c.NewHandler(mux, &http2.Server{})))
}
