// SPDX-License-Identifier: AGPL-3.0-or-later
package proxy

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGunzipIsLazyAndMalformedConsumedBodyIs400(t *testing.T) {
	for _, path := range []string{"/unknown", "/proxy/spotify/api/token"} {
		calls := 0
		h, _ := providerHandler(t, func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(200) })
		r := httptest.NewRequest("POST", path, strings.NewReader("invalid gzip"))
		r.Header.Set("Content-Encoding", "gzip")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		want := 400
		if path == "/unknown" {
			want = 404
		}
		if w.Code != want || calls != 0 {
			t.Fatalf("lazy compressed body changed response/effects for %s", path)
		}
	}
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, _ = writer.Write([]byte("synthetic=body"))
	_ = writer.Close()
	h, _ := providerHandler(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if string(body) != "synthetic=body" || r.Header.Get("Content-Encoding") != "" {
			t.Error("compressed body not forwarded as reference plaintext")
		}
	})
	r := httptest.NewRequest("POST", "/proxy/spotify/api/token", &compressed)
	r.Header.Set("Content-Encoding", "gzip")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal("valid compression rejected")
	}
}

func TestRawRedirectIsNotFollowedAndBufferedRedirectHasNoReferer(t *testing.T) {
	for _, path := range []string{"/proxy/giphy/v1/gifs/search", "/proxy/spotify/api/token"} {
		attempts := 0
		var base string
		h, provider := providerHandler(t, func(w http.ResponseWriter, r *http.Request) {
			attempts++
			if r.Header.Get("Referer") != "" || r.Header.Get("Idempotency-Key") != "" {
				t.Error("transport added source-absent header")
			}
			if attempts == 1 {
				w.Header().Set("Location", base+"/redirect-target")
				w.WriteHeader(302)
			} else {
				w.WriteHeader(200)
			}
		})
		base = provider
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		wantStatus, wantCalls := 302, 1
		if strings.Contains(path, "spotify") {
			wantStatus, wantCalls = 200, 2
		}
		if w.Code != wantStatus || attempts != wantCalls {
			t.Fatal("provider redirect behavior changed")
		}
	}
}

func TestMetricsRouteLabelsCountersAndVersionRejections(t *testing.T) {
	h := newHandler(Config{}, &Secrets{}, http.DefaultTransport)
	for _, path := range []string{"/i/status", "/unknown", "/proxy/soundcloud/stream?url=synthetic", "/v99/proxy/giphy/v1/gifs"} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", path, nil))
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/i/metrics", nil))
	text := w.Body.String()
	for _, expected := range []string{
		`http_request_duration_seconds_count{handler="/i/status",method="GET",status_code="200"} 1`,
		`http_request_duration_seconds_count{handler="N/A",method="GET",status_code="404"} 1`,
		`http_request_duration_seconds_count{handler="/proxy/soundcloud/stream",method="GET",status_code="500"} 1`,
		"net_errors 1.0", "# TYPE gc_bytes_allocated_total gauge", `le="+Inf"`,
	} {
		if !strings.Contains(text, expected) {
			t.Errorf("missing metric %s", expected)
		}
	}
	if len(h.metrics.latencies) != 3 || w.Header().Get("Content-Type") != "text/plain; version=0.0.4" {
		t.Fatal("version error entered metrics or metrics type changed")
	}
}

func TestLogFormatsThresholdAndInjectedWhitespace(t *testing.T) {
	for _, format := range logFormats {
		var output bytes.Buffer
		logger := NewLogger(Config{LogLevel: "Warn", LogFormat: &format}, &output)
		logger.Event("Info", "must be excluded")
		logger.Event("Error", "message\nforged line", logField{"request", "synthetic-request"}, logField{"code", "500"})
		if strings.Count(output.String(), "\n") != 1 || strings.Contains(output.String(), "must be excluded") {
			t.Fatal("log threshold or line injection protection failed")
		}
		if format == "JSON" || format == "StructuredJSON" {
			var object map[string]any
			if json.Unmarshal(output.Bytes(), &object) != nil || object["request"] != "synthetic-request" || object["code"] != "500" {
				t.Fatal("structured fields lost")
			}
			if format == "StructuredJSON" && object["level"] != "Error" {
				t.Fatal("log severity lost")
			}
		} else if format == "Netstring" && !strings.Contains(output.String(), "4:code,1:=,3:500,") {
			t.Fatal("Tinylog netstring element framing changed")
		}
	}
}

func TestShutdownDrainsInFlightRequestAndCancelsCollectors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server, logger := NewServer(Config{Proxy: Endpoint{"127.0.0.1", 0}}, &Secrets{}, ctx)
	logger.output = io.Discard
	started, release := make(chan struct{}), make(chan struct{})
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		_, _ = w.Write([]byte("synthetic"))
	})
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	finished := make(chan error, 1)
	go func() {
		response, err := http.Get("http://" + listener.Addr().String())
		if err == nil {
			payload, readErr := io.ReadAll(response.Body)
			_ = response.Body.Close()
			err = readErr
			if string(payload) != "synthetic" {
				err = io.ErrUnexpectedEOF
			}
		}
		finished <- err
	}()
	<-started
	cancel()
	shutdown := make(chan error, 1)
	go func() {
		deadline, stop := context.WithTimeout(context.Background(), 2*time.Second)
		defer stop()
		shutdown <- server.Shutdown(deadline)
	}()
	select {
	case <-shutdown:
		t.Fatal("shutdown dropped in-flight request")
	case <-time.After(25 * time.Millisecond):
	}
	close(release)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if err := <-shutdown; err != nil {
		t.Fatal(err)
	}
	if err := <-served; err != http.ErrServerClosed {
		t.Fatal(err)
	}
}
