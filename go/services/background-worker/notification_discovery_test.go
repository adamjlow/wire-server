// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNativeNotificationJitterAndCapOrdering(t *testing.T) {
	low := func(int64) int64 { return 0 }
	high := func(n int64) int64 { return n - 1 }
	for _, v := range []struct {
		iteration uint64
		low, high int64
	}{{0, 5000, 10000}, {1, 10000, 20000}, {17, 655360000, 1310720000}, {63, 4611686018427387903, 9223372036854775806}} {
		if got := nativeNotificationDelay(10000, v.iteration, low); got != v.low {
			t.Fatal("native jitter lower endpoint", got, v.low)
		}
		if got := nativeNotificationDelay(10000, v.iteration, high); got != v.high {
			t.Fatal("native jitter upper endpoint", got, v.high)
		}
	}
	if got := min(nativeNotificationDelay(10000, 17, low), int64(300000000)); got != 300000000 {
		t.Fatal("source cap is after jitter", got)
	}
}
func TestDiscoveryRetryCumulativeBudgetAndCancellation(t *testing.T) {
	calls, waits := 0, 0
	var cumulative time.Duration
	failure := errors.New("synthetic private backend error")
	_, err := retryQueueDiscovery(t.Context(), func() ([]string, error) { calls++; return nil, failure }, func(n int64) int64 { return n - 1 }, func(_ context.Context, d time.Duration) bool { waits++; cumulative += d; return true })
	if !errors.Is(err, failure) || calls != 13 || waits != 12 || cumulative != 40950*time.Millisecond {
		t.Fatal("source accumulated-wait budget changed", calls, waits, cumulative, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	_, err = retryQueueDiscovery(ctx, func() ([]string, error) { cancel(); return nil, failure }, func(int64) int64 { t.Fatal("cancelled discovery consulted retry policy"); return 0 }, func(context.Context, time.Duration) bool { t.Fatal("cancelled discovery waited"); return false })
	if !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation retried", err)
	}
}
func TestQueueDiscoveryRestartsPaginationAndUsesSourcePageOrder(t *testing.T) {
	t.Setenv("RABBITMQ_USERNAME", "synthetic-user")
	t.Setenv("RABBITMQ_PASSWORD", "synthetic-password")
	var requests []string
	var mu sync.Mutex
	server, endpoint := brigAccountPeer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		user, password, ok := r.BasicAuth()
		if !ok || user != "synthetic-user" || password != "synthetic-password" {
			t.Error("discovery authentication lost")
		}
		page := r.URL.Query().Get("page")
		mu.Lock()
		requests = append(requests, page)
		call := len(requests)
		mu.Unlock()
		if r.URL.Path != "/api/queues/backendA" || r.URL.Query().Get("page_size") != "100" || r.URL.Query().Get("use_regex") != "true" {
			t.Error("source discovery request changed")
		}
		switch call {
		case 1:
			_, _ = io.WriteString(w, `{"page":1,"page_count":2,"items":[{"name":"backend-notifications.stale.example.com","vhost":"backendA"}]}`)
		case 2:
			w.WriteHeader(500)
		case 3:
			_, _ = io.WriteString(w, `{"page":1.0,"page_count":2e0,"items":[{"name":"backend-notifications.first.example.com","vhost":"backendA"}],"padding":"`+strings.Repeat("x", 8<<20)+`"}`)
		case 4:
			_, _ = io.WriteString(w, `{"page":2,"page_count":2,"items":[{"name":"backend-notifications.second.example.com","vhost":"backendA"}]}`)
		default:
			t.Error("unexpected discovery retry")
		}
	})
	service := New(Config{}, GalleyConfig{})
	service.Config.Rabbit.AdminHost = endpoint.Host
	service.Config.Rabbit.AdminPort = endpoint.Port
	service.Config.Rabbit.VHost = "backendA"
	ctx, stop := context.WithTimeout(t.Context(), 5*time.Second)
	defer stop()
	got, err := service.remoteDomains(ctx, server.Client())
	mu.Lock()
	defer mu.Unlock()
	if err != nil || !reflect.DeepEqual(got, []string{"second.example.com", "first.example.com"}) || !reflect.DeepEqual(requests, []string{"1", "2", "1", "2"}) {
		t.Fatal("whole pagination retry/order changed", got, requests, err)
	}
}
func TestQueueDiscoveryNativeRequiredFields(t *testing.T) {
	for _, body := range []string{`{"page":1,"page_count":1,"items":[{"Name":"queue","vhost":"synthetic"}]}`, `{"page":1,"page_count":1,"items":null}`, `{"page":1,"page_count":1,"items":[{"name":"queue"}]}`, `{"page":null,"page_count":1,"items":[]}`, `{"page":1.5,"page_count":1,"items":[]}`, `{"page":1,"page_count":1,"items":[{"name":null,"vhost":"synthetic"}]}`} {
		if _, err := decodeQueueDiscoveryPage([]byte(body)); err == nil {
			t.Fatal("invalid native queue page accepted")
		}
	}
	if _, err := decodeQueueDiscoveryPage([]byte(`{"page":5.0,"page_count":-1,"items":[]}`)); err != nil {
		t.Fatal("source signed integral page metadata rejected", err)
	}
}
