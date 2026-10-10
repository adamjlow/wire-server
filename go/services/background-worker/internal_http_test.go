// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestInternalHTTPHeadersTimeoutWithoutBodyDeadline(t *testing.T) {
	t.Setenv("http_proxy", "")
	t.Setenv("https_proxy", "")
	t.Setenv("no_proxy", "")
	transport, err := newInternalHTTPTransport()
	if err != nil {
		t.Fatal(err)
	}
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

func TestInternalHTTPReusedIncompleteHeadersAndNoUnsafeReplay(t *testing.T) {
	for _, test := range []struct {
		name                     string
		warm, oneShot, malformed bool
		wantSuccess              bool
	}{
		{"reused-incomplete-headers", true, false, false, true},
		{"fresh-incomplete-headers", false, false, false, false},
		{"one-shot-push", true, true, false, false},
		{"malformed-status-line", true, false, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("http_proxy", "")
			t.Setenv("https_proxy", "")
			t.Setenv("no_proxy", "")
			var calls atomic.Int32
			failCall := int32(1)
			if test.warm {
				failCall = 2
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				if calls.Add(1) != failCall {
					_, _ = io.WriteString(w, "ok")
					return
				}
				connection, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				defer connection.Close()
				if test.malformed {
					_, _ = io.WriteString(connection, "not HTTP\r\n\r\n")
				} else {
					_, _ = io.WriteString(connection, "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\n")
				}
			}))
			defer server.Close()
			transport, err := newInternalHTTPTransport()
			if err != nil {
				t.Fatal(err)
			}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport}
			if test.warm {
				request, _ := http.NewRequestWithContext(t.Context(), "GET", server.URL, nil)
				response, err := client.Do(request)
				if err != nil {
					t.Fatal(err)
				}
				_, _ = io.Copy(io.Discard, response.Body)
				response.Body.Close()
			}
			request, _ := http.NewRequestWithContext(t.Context(), "GET", server.URL, nil)
			if test.oneShot {
				request, _ = http.NewRequestWithContext(t.Context(), "POST", server.URL, io.NopCloser(strings.NewReader("synthetic")))
				request.ContentLength = -1
			}
			response, err := client.Do(request)
			if response != nil {
				_, _ = io.Copy(io.Discard, response.Body)
				response.Body.Close()
			}
			if (err == nil) != test.wantSuccess {
				t.Fatalf("success=%t, expected=%t", err == nil, test.wantSuccess)
			}
			wantCalls := failCall
			if test.wantSuccess {
				wantCalls++
			}
			if calls.Load() != wantCalls {
				t.Fatal("fresh, malformed or one-shot request replayed incorrectly")
			}
		})
	}
}
