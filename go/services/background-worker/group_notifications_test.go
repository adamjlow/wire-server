// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"
)

type observedNotificationQueue struct {
	mu      sync.Mutex
	targets []string
	bundles []payloadBundle
}

func (q *observedNotificationQueue) Enqueue(_ context.Context, target string, bundle payloadBundle) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.targets, q.bundles = append(q.targets, target), append(q.bundles, bundle)
	return nil
}

func TestGroupJoinDeliversSourceLocalEventAndModernLegacyRemoteBundles(t *testing.T) {
	queue := &observedNotificationQueue{}
	now := time.Date(2026, 10, 9, 12, 13, 14, 123456789, time.UTC)
	var pushes []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(queue.bundles) != 1 {
			t.Error("local push preceded remote enqueue")
		}
		if r.Method != "POST" || r.URL.Path != "/i/push/v2" || r.Header.Get("Request-Id") != "synthetic-request" || r.ContentLength != -1 {
			t.Error("source chunked push request changed")
		}
		if err := json.NewDecoder(r.Body).Decode(&pushes); err != nil {
			t.Error(err)
		}
		w.WriteHeader(200)
	}))
	defer server.Close()
	host, p, _ := net.SplitHostPort(server.Listener.Addr().String())
	port, _ := strconv.Atoi(p)
	n := &conversationJoinNotifier{Queue: queue, Client: server.Client(), Gundeck: Endpoint{host, port}, Domain: "example.com", Now: func() time.Time { return now }}
	join := groupJoin{Conversation: &storedConversation{ID: jobUUID, Team: ptr(jobUUID), Metadata: map[string]any{"cells_state": int32(1)}}, RequestID: "synthetic-request", Actor: qualifiedUser{ID: "00000000-0000-0000-0000-000000000000", Domain: "example.com"}, Via: "scim", NewUsers: []qualifiedUser{{ID: jobUUID, Domain: "example.com"}}, Locals: []string{jobUUID}, Remotes: []qualifiedUser{{ID: jobUUID, Domain: "remote.example.com"}, {ID: jobUUID, Domain: "example.com"}}}
	if err := n.NotifyJoin(t.Context(), join); err != nil {
		t.Fatal(err)
	}
	if len(queue.targets) != 1 || queue.targets[0] != "remote.example.com" || len(pushes) != 1 {
		t.Fatal("source target buckets/origin suppression changed")
	}
	push := pushes[0]
	if push["origin"] != join.Actor.ID || push["is_cells_event"] != true || push["transient"] != nil || push["origin_connection"] != nil {
		t.Fatal("source push defaults changed")
	}
	event := push["payload"].([]any)[0].(map[string]any)
	if event["type"] != "conversation.member-join" || event["via"] != "scim" || event["from"] != join.Actor.ID || event["time"] != "2026-10-09T12:13:14.123Z" || event["team"] != jobUUID {
		t.Fatal("source event serialization changed")
	}
	data := event["data"].(map[string]any)
	if data["add_type"] != "external_add" || len(data["users"].([]any)) != 1 || len(data["user_ids"].([]any)) != 1 {
		t.Fatal("source join action changed")
	}
	bundle := queue.bundles[0]
	if len(bundle.Notifications) != 2 || bundle.Policy == nil || *bundle.Policy != "keep_queued" {
		t.Fatal("source bundle policy changed")
	}
	for i, notification := range bundle.Notifications {
		var body map[string]any
		if json.Unmarshal([]byte(*notification.Body), &body) != nil || *notification.Component != "galley" || *notification.Path != "on-conversation-updated" || *notification.RequestID != join.RequestID {
			t.Fatal("source remote notification envelope changed")
		}
		if i == 0 {
			if *notification.Versions.From != 1 || notification.Versions.Until != nil || body["convId"] != jobUUID || body["extraConversationData"].(map[string]any)["group_id"] != nil || body["action"].(map[string]any)["tag"] != "ConversationJoinTag" {
				t.Fatal("source modern join representation changed")
			}
		} else if *notification.Versions.From != 0 || *notification.Versions.Until != 1 || body["cuConvId"] != jobUUID || body["cuAction"].(map[string]any)["action"].(map[string]any)["join_type"] != "external_add" || body["cuTime"] == nil {
			t.Fatal("source legacy join representation changed")
		}
	}
}

func TestGroupPushChunkingAndEmptyRecipientCellsDelivery(t *testing.T) {
	for _, count := range []int{0, 130, 2001} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			var mu sync.Mutex
			requests, total := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var pushes []struct {
					Recipients []any `json:"recipients"`
					Cells      bool  `json:"is_cells_event"`
				}
				if err := json.NewDecoder(r.Body).Decode(&pushes); err != nil || len(pushes) != 1 {
					t.Error("invalid push batch")
					return
				}
				if len(pushes[0].Recipients) > 128 || !pushes[0].Cells {
					t.Error("source chunk bound/Cells flag changed")
				}
				mu.Lock()
				requests++
				total += len(pushes[0].Recipients)
				mu.Unlock()
			}))
			defer server.Close()
			host, p, _ := net.SplitHostPort(server.Listener.Addr().String())
			port, _ := strconv.Atoi(p)
			n := &conversationJoinNotifier{Client: server.Client(), Gundeck: Endpoint{host, port}, Domain: "example.com"}
			join := groupJoin{Conversation: &storedConversation{ID: jobUUID, Metadata: map[string]any{"cells_state": int32(2)}}, Actor: qualifiedUser{ID: jobUUID, Domain: "example.com"}, Locals: []string{}}
			for i := 0; i < count; i++ {
				join.Locals = append(join.Locals, fmt.Sprintf("%08x-1111-4111-8111-111111111111", i))
			}
			if err := n.pushLocal(t.Context(), join, map[string]any{"type": "conversation.member-join"}); err != nil {
				t.Fatal(err)
			}
			wantRequests, wantTotal := 1, count
			if count == 130 {
				wantRequests = 2
			} else if count == 2001 {
				wantRequests, wantTotal = 0, 0
			}
			if requests != wantRequests || total != wantTotal {
				t.Fatal("empty push, chunking or large-fanout suppression changed")
			}
		})
	}
}
