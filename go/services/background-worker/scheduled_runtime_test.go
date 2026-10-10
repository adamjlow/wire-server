// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"
)

func TestScheduledCapabilityRequestsKeepEachJobRequestID(t *testing.T) {
	var mu sync.Mutex
	calls := map[string]int{}
	service, client := testFederator(t, func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("Request-Id")
		if id != "synthetic-first" && id != "synthetic-second" {
			t.Error("job request scope lost")
		}
		mu.Lock()
		calls[id]++
		mu.Unlock()
		_, _ = io.WriteString(w, `{"supported":[],"supported_versions":[4]}`)
	})
	notifier := &adminlessNotifier{Base: conversationJoinNotifier{Domain: "example.com"}, Federation: service, FederationClient: client}
	var wg sync.WaitGroup
	for _, id := range []string{"synthetic-first", "synthetic-second"} {
		wg.Go(func() {
			job := adminlessJobNotifications{adminlessNotifier: notifier, RequestID: id}
			supported, err := job.SupportsSystemDelete(t.Context(), "remote.example.com")
			if err != nil || !supported {
				t.Error("capability request failed", err)
			}
		})
	}
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if calls["synthetic-first"] != 2 || calls["synthetic-second"] != 2 {
		t.Fatal("native two-call negotiation/request scope changed", calls)
	}
}
func TestScheduledNotificationChannelAbsenceIsBoundedAndCancelled(t *testing.T) {
	queue := &scheduledNotificationQueue{ready: make(chan struct{})}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := queue.Enqueue(ctx, "remote.example.com", payloadBundle{}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled enqueue waited for channel", err)
	}
	ctx, cancel = context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	start := time.Now()
	if err := queue.Enqueue(ctx, "remote.example.com", payloadBundle{}); err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("native one-second unavailable channel boundary missing", err)
	}
	if time.Since(start) < 900*time.Millisecond {
		t.Fatal("channel absence did not wait for recovery")
	}
}
