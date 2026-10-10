// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestInternalHTTPHeadersTimeoutWithoutBodyDeadline(t *testing.T) {
	transport := newInternalHTTPTransport()
	if transport.budget != 30*time.Second {
		t.Fatal("native manager default timeout changed")
	}
	transport.budget = 100 * time.Millisecond
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	t.Run("headers-stalled", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
		defer server.Close()
		req, _ := http.NewRequestWithContext(t.Context(), "GET", server.URL, nil)
		response, err := client.Do(req)
		if response != nil {
			response.Body.Close()
		}
		var timeout net.Error
		if !errors.As(err, &timeout) || !timeout.Timeout() || !retryAdminlessBrigTransport(err) {
			t.Fatal("default header timeout did not retain retryable network classification")
		}
	})
	t.Run("body-outlives-header-budget", func(t *testing.T) {
		release := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "2")
			w.WriteHeader(200)
			w.(http.Flusher).Flush()
			select {
			case <-release:
				_, _ = io.WriteString(w, "ok")
			case <-r.Context().Done():
			}
		}))
		defer server.Close()
		req, _ := http.NewRequestWithContext(t.Context(), "GET", server.URL, nil)
		response, err := client.Do(req)
		if err != nil {
			close(release)
			t.Fatal(err)
		}
		defer response.Body.Close()
		// Native BodyReader has no response timeout after headers. Real progress
		// after the scaled header budget must still reach the caller.
		timer := time.AfterFunc(2*transport.budget, func() { close(release) })
		defer timer.Stop()
		body, err := io.ReadAll(response.Body)
		if err != nil || string(body) != "ok" {
			t.Fatal("header budget incorrectly became a total body deadline")
		}
	})
	t.Run("caller-cancellation-survives-headers", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "2")
			w.WriteHeader(200)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		}))
		defer server.Close()
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, "GET", server.URL, nil)
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		cancel()
		if _, err := io.ReadAll(response.Body); !errors.Is(err, context.Canceled) {
			t.Fatal("caller cancellation was detached from response body")
		}
	})
}
