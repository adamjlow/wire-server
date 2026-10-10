// SPDX-License-Identifier: AGPL-3.0-only
// Qualification-only h2c sink: holds actual delivery until cancellation.
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync/atomic"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

func main() {
	var entered atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/qualification/status", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]int64{"entered": entered.Load()})
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
		entered.Add(1)
		<-r.Context().Done()
	})
	log.Fatal(http.ListenAndServe("127.0.0.1:16197", h2c.NewHandler(mux, &http2.Server{})))
}
