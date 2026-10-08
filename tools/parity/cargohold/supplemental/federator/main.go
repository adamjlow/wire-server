// Synthetic local h2c boundary forwarding to real Haskell/Go cargohold peers.
package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

func main() {
	var lock sync.Mutex
	target := "8085"
	var held bool
	var release chan struct{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/control" {
			var value struct {
				Target string `json:"target"`
				Hold   *bool  `json:"hold"`
			}
			if r.Method != "POST" || json.NewDecoder(io.LimitReader(r.Body, 128)).Decode(&value) != nil || ((value.Target != "" && value.Target != "8084" && value.Target != "8085") || (value.Target == "" && value.Hold == nil)) {
				w.WriteHeader(400)
				return
			}
			lock.Lock()
			if value.Target != "" {
				target = value.Target
			}
			if value.Hold != nil {
				if *value.Hold && !held {
					release = make(chan struct{})
					held = true
				}
				if !*value.Hold && held {
					close(release)
					held = false
				}
			}
			lock.Unlock()
			w.WriteHeader(200)
			return
		}
		if r.URL.Path == "/rpc/foreign.example.com/brig/api-version" && r.ProtoMajor == 2 && r.Method == "POST" {
			evidence := map[string]any{"protocol": r.ProtoMajor, "route": "api-version", "origin": r.Header.Get("Wire-Origin-Domain"), "request_id": r.Header.Get("Wire-Origin-Request-Id"), "version": r.Header.Get("X-Wire-API-Version")}
			encoded, _ := json.Marshal(evidence)
			log.Print(string(encoded))
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"supported_versions":[0,1,2,3,4],"supported":[0,1]}`))
			return
		}
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if r.ProtoMajor != 2 || r.Method != "POST" || len(parts) != 4 || parts[0] != "rpc" || parts[1] != "foreign.example.com" || parts[2] != "cargohold" || (parts[3] != "get-asset" && parts[3] != "stream-asset") {
			w.WriteHeader(400)
			return
		}
		lock.Lock()
		port := target
		lock.Unlock()
		body, err := io.ReadAll(io.LimitReader(r.Body, 4097))
		if err != nil || len(body) > 4096 {
			w.WriteHeader(400)
			return
		}
		var payload map[string]any
		if json.Unmarshal(body, &payload) != nil {
			w.WriteHeader(400)
			return
		}
		evidence := map[string]any{"protocol": r.ProtoMajor, "route": parts[3], "target_port": port, "origin": r.Header.Get("Wire-Origin-Domain"), "request_id": r.Header.Get("Wire-Origin-Request-Id"), "user": payload["user"], "key": payload["key"], "token_present": payload["token"] != nil, "accept": r.Header.Get("Accept"), "content_type": r.Header.Get("Content-Type"), "version": r.Header.Get("X-Wire-API-Version")}
		encoded, _ := json.Marshal(evidence)
		log.Print(string(encoded))
		req, err := http.NewRequestWithContext(r.Context(), "POST", "http://127.0.0.1:"+port+"/federation/"+parts[3], strings.NewReader(string(body)))
		if err != nil {
			w.WriteHeader(500)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Wire-Origin-Domain", r.Header.Get("Wire-Origin-Domain"))
		req.Header.Set("Request-Id", r.Header.Get("Wire-Origin-Request-Id"))
		req.Header.Set("Wire-Origin-IP", "192.0.2.10")
		client := http.Client{Timeout: 35 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			w.WriteHeader(502)
			return
		}
		defer resp.Body.Close()
		for k, values := range resp.Header {
			if k != "Content-Length" {
				w.Header()[k] = values
			}
		}
		w.Header().Set("Wire-Remote-IP", "192.0.2.20")
		w.WriteHeader(resp.StatusCode)
		lock.Lock()
		wait := release
		hold := held
		lock.Unlock()
		if parts[3] == "stream-asset" && hold && resp.StatusCode == 200 {
			first := make([]byte, 8192)
			n, err := io.ReadFull(resp.Body, first)
			if err != nil && err != io.ErrUnexpectedEOF {
				return
			}
			if _, err := w.Write(first[:n]); err != nil {
				return
			}
			w.(http.Flusher).Flush()
			select {
			case <-wait:
			case <-r.Context().Done():
				return
			}
		}
		_, _ = io.Copy(w, io.LimitReader(resp.Body, 28*1024*1024+1))
	})
	server := http.Server{Addr: "127.0.0.1:8097", Handler: h2c.NewHandler(handler, &http2.Server{}), ReadHeaderTimeout: 10 * time.Second}
	log.SetFlags(0)
	log.Fatal(server.ListenAndServe())
}
