// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptrace"
	"sync"
	"time"
)

// http-client0.7.19 defaults to30s for acquiring a connection and then the
// remaining budget for response headers. Request writes and response bodies
// are not included. Keep caller cancellation alive until the body closes.
type internalHTTPTransport struct {
	base   *http.Transport
	budget time.Duration
}

func newInternalHTTPTransport() *internalHTTPTransport {
	return &internalHTTPTransport{base: &http.Transport{Proxy: nil, MaxIdleConns: 512, MaxIdleConnsPerHost: 10}, budget: 30 * time.Second}
}
func (t *internalHTTPTransport) CloseIdleConnections() { t.base.CloseIdleConnections() }

type internalHTTPTimeout struct{}

func (internalHTTPTimeout) Error() string   { return "internal HTTP response timed out" }
func (internalHTTPTimeout) Timeout() bool   { return true }
func (internalHTTPTimeout) Temporary() bool { return true }

func (t *internalHTTPTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	ctx, cancel := context.WithCancelCause(request.Context())
	var mu sync.Mutex
	var timer *time.Timer
	var connectionStart time.Time
	remaining := t.budget
	finished := false
	stopTimer := func() {
		if timer != nil {
			timer.Stop()
		}
	}
	startTimer := func(duration time.Duration) {
		stopTimer()
		timer = time.AfterFunc(duration, func() { cancel(internalHTTPTimeout{}) })
	}
	trace := &httptrace.ClientTrace{
		GetConn: func(string) {
			mu.Lock()
			defer mu.Unlock()
			if finished {
				return
			}
			connectionStart = time.Now()
			startTimer(t.budget)
		},
		GotConn: func(httptrace.GotConnInfo) {
			mu.Lock()
			defer mu.Unlock()
			if finished {
				return
			}
			stopTimer()
			remaining = t.budget - time.Since(connectionStart)
			if remaining <= 0 {
				cancel(internalHTTPTimeout{})
			}
		},
		WroteRequest: func(httptrace.WroteRequestInfo) {
			mu.Lock()
			defer mu.Unlock()
			if finished {
				return
			}
			startTimer(remaining)
		},
	}
	response, err := t.base.RoundTrip(request.Clone(httptrace.WithClientTrace(ctx, trace)))
	mu.Lock()
	finished = true
	stopTimer()
	mu.Unlock()
	if err != nil {
		cause := context.Cause(ctx)
		cancel(nil)
		var timeout internalHTTPTimeout
		if errors.As(cause, &timeout) {
			return nil, timeout
		}
		return nil, err
	}
	response.Body = &internalHTTPBody{ReadCloser: response.Body, cancel: cancel}
	return response, nil
}

type internalHTTPBody struct {
	io.ReadCloser
	cancel context.CancelCauseFunc
}

func (b *internalHTTPBody) Close() error {
	err := b.ReadCloser.Close()
	b.cancel(nil)
	return err
}
