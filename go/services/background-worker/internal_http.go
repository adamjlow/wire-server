// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"os"
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

func newInternalHTTPTransport() (*internalHTTPTransport, error) {
	proxy, err := internalHTTPProxy(os.Environ())
	if err != nil {
		return nil, err
	}
	return &internalHTTPTransport{base: &http.Transport{Proxy: proxy, MaxIdleConns: 512, MaxIdleConnsPerHost: 10}, budget: 30 * time.Second}, nil
}
func (t *internalHTTPTransport) CloseIdleConnections() { t.base.CloseIdleConnections() }

type internalHTTPTimeout struct{}

func (internalHTTPTimeout) Error() string   { return "internal HTTP response timed out" }
func (internalHTTPTimeout) Timeout() bool   { return true }
func (internalHTTPTimeout) Temporary() bool { return true }

func (t *internalHTTPTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	current := request
	for {
		response, err, reused := t.roundTrip(current)
		if err == nil || !reused || request.Context().Err() != nil || !retryInternalHTTPConnection(err) ||
			(request.Body != nil && request.Body != http.NoBody && request.GetBody == nil) {
			return response, err
		}
		// Native manager discards a reused broken connection and reacquires.
		// Replay only bodies that are replayable; Gundeck's one-shot chunked
		// push intentionally has no GetBody and no application-level retry.
		current = request.Clone(request.Context())
		if request.GetBody != nil {
			current.Body, err = request.GetBody()
			if err != nil {
				return nil, errors.New("internal HTTP request replay failed")
			}
		}
	}
}

func retryInternalHTTPConnection(err error) bool {
	var network net.Error
	if errors.As(err, &network) && network.Timeout() {
		return false
	}
	var operation *net.OpError
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.As(err, &operation)
}

func (t *internalHTTPTransport) roundTrip(request *http.Request) (*http.Response, error, bool) {
	ctx, cancel := context.WithCancelCause(request.Context())
	var mu sync.Mutex
	var timer *time.Timer
	var connectionStart time.Time
	remaining := t.budget
	finished := false
	reused := false
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
		GotConn: func(info httptrace.GotConnInfo) {
			mu.Lock()
			defer mu.Unlock()
			if finished {
				return
			}
			stopTimer()
			reused = info.Reused
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
	lastReused := reused
	stopTimer()
	mu.Unlock()
	if err != nil {
		cause := context.Cause(ctx)
		cancel(nil)
		var timeout internalHTTPTimeout
		if errors.As(cause, &timeout) {
			return nil, timeout, lastReused
		}
		return nil, err, lastReused
	}
	response.Body = &internalHTTPBody{ReadCloser: response.Body, cancel: cancel}
	return response, nil, lastReused
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
