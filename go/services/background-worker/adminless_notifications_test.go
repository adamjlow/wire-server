// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

type adminlessObservedBots struct {
	calls  int
	before func()
}

func (b *adminlessObservedBots) DeliverAsync(_ context.Context, bots []localConversationMember, event map[string]any) {
	b.calls++
	if b.before != nil {
		b.before()
	}
}

func TestAdminlessNotificationsSourceOriginRoutingAndBundles(t *testing.T) {
	for _, system := range []bool{true, false} {
		for _, kind := range []string{"member-update", "delete", "adminless-reminder"} {
			t.Run(kind+map[bool]string{true: "-system", false: "-user"}[system], func(t *testing.T) {
				queue := &observedNotificationQueue{}
				var pushes []map[string]any
				server, endpoint := brigAccountPeer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					queue.mu.Lock()
					count := len(queue.bundles)
					queue.mu.Unlock()
					if count != 1 {
						t.Error("local delivery preceded federation enqueue")
					}
					if r.URL.Path != "/i/push/v2" || r.Header.Get("Request-Id") != "synthetic" || r.ContentLength != -1 {
						t.Error("push RPC contract changed")
					}
					if err := json.NewDecoder(r.Body).Decode(&pushes); err != nil {
						t.Error(err)
					}
					w.WriteHeader(200)
				}))
				bots := &adminlessObservedBots{before: func() {
					if len(pushes) != 1 {
						t.Error("bots delivered before local push")
					}
				}}
				now := time.Date(2026, 10, 9, 10, 11, 12, 123456789, time.UTC)
				n := &adminlessNotifier{Base: conversationJoinNotifier{Client: server.Client(), Gundeck: endpoint, Queue: queue, Bots: bots, Domain: "example.com", Now: func() time.Time { return now }}}
				botID := "00000000-0000-0000-0000-000000000002"
				conv := &storedConversation{ID: jobUUID, Team: stringPtr(jobUUID), Metadata: map[string]any{"cells_state": int32(0)}, LocalMembers: []localConversationMember{{ID: jobUUID}, {ID: botID, Service: stringPtr(jobUUID), Provider: stringPtr(jobUUID)}}, RemoteMembers: []remoteConversationMember{{User: qualifiedUser{ID: jobUUID, Domain: "remote.example.com"}}, {User: qualifiedUser{ID: jobUUID, Domain: "remote.example.com"}}}}
				if !system && kind == "adminless-reminder" {
					conv.Metadata["cells_state"] = int32(1)
				}
				originalCells := conv.Metadata["cells_state"]
				var actor *string
				if !system {
					actor = stringPtr(jobUUID)
				}
				var err error
				switch kind {
				case "member-update":
					err = n.Promoted(t.Context(), conv, qualifiedUser{ID: jobUUID, Domain: "example.com"}, actor, "synthetic")
				case "delete":
					err = n.Deleted(t.Context(), conv, actor, "synthetic")
				case "adminless-reminder":
					err = n.Reminder(t.Context(), conv, actor, "synthetic", "400000000001970-01-01T00:00:00.123Z")
				}
				if err != nil {
					t.Fatal(err)
				}
				if len(pushes) != 1 || len(queue.bundles) != 1 || queue.targets[0] != "remote.example.com" {
					t.Fatal("recipient bucket mismatch")
				}
				push := pushes[0]
				event := push["payload"].([]any)[0].(map[string]any)
				expectedType := "conversation." + kind
				if system {
					expectedType = "conversation.system." + kind
				}
				if event["type"] != expectedType || event["conversation"] != jobUUID || event["team"] != jobUUID || event["time"] != "2026-10-09T10:11:12.123Z" {
					t.Fatal("event shape mismatch")
				}
				if system {
					if event["via"] != "system" || event["from"] != nil || event["qualified_from"] != nil || push["origin"] != nil || push["is_cells_event"] != true {
						t.Fatal("system origin/Cells contract changed")
					}
				} else {
					if event["via"] != "user" || event["from"] != jobUUID || push["origin"] != jobUUID || push["is_cells_event"] != false {
						t.Fatal("user origin/Cells contract changed")
					}
				}
				botDelivery := !system && kind != "adminless-reminder"
				wantRecipients := 2
				wantBots := 0
				if botDelivery {
					wantRecipients = 1
					wantBots = 1
				}
				if len(push["recipients"].([]any)) != wantRecipients || bots.calls != wantBots {
					t.Fatal("bot versus local-recipient distinction changed")
				}
				if conv.Metadata["cells_state"] != originalCells {
					t.Fatal("routing mutated stored conversation")
				}
				bundle := queue.bundles[0]
				if system || kind == "adminless-reminder" {
					path := "on-conversation-" + kind
					if system {
						path = "on-conversation-system-" + kind
					}
					if *bundle.Policy != "drop_if_unsupported" || len(bundle.Notifications) != 1 || *bundle.Notifications[0].Path != path || *bundle.Notifications[0].Versions.From != 4 {
						t.Fatal("V4/drop notification contract changed")
					}
				} else {
					if *bundle.Policy != "keep_queued" || len(bundle.Notifications) != 2 || *bundle.Notifications[0].Versions.From != 1 || *bundle.Notifications[1].Versions.From != 0 || *bundle.Notifications[1].Versions.Until != 1 {
						t.Fatal("modern/legacy action versions changed")
					}
				}
				var remote map[string]any
				if json.Unmarshal([]byte(*bundle.Notifications[0].Body), &remote) != nil {
					t.Fatal("invalid remote body")
				}
				recipientField := "alreadyPresentUsers"
				if system || kind == "adminless-reminder" {
					recipientField = "already_present_users"
					if remote["alreadyPresentUsers"] != nil {
						t.Fatal("V4 fields must use native CustomEncoded snake_case")
					}
				}
				if !reflect.DeepEqual(remote[recipientField], []any{jobUUID}) {
					t.Fatal("remote recipients not deduplicated")
				}
				if kind == "adminless-reminder" {
					reminder := remote["reminder"].(map[string]any)
					if reminder["deletion_scheduled_for"] != "400000000001970-01-01T00:00:00.123Z" || event["data"].(map[string]any)["deletion_scheduled_for"] != reminder["deletion_scheduled_for"] {
						t.Fatal("extended timestamp narrowed")
					}
					if system && remote["orig_user_id"] != nil || !system && remote["orig_user_id"] == nil {
						t.Fatal("reminder origin mismatch")
					}
				}
			})
		}
	}
}

func TestAdminlessCapabilityNativeNegotiationAndIntegerSchema(t *testing.T) {
	for _, test := range []struct {
		name, first, second string
		want                bool
		calls               int
		bad                 bool
	}{
		{"large-source-valid", `{"padding":"` + strings.Repeat("x", 8<<20) + `","supported":[],"supported_versions":[4]}`, `{"supported":[],"supported_versions":[4]}`, true, 2, false},
		{"supported", `{"supported":[0,1],"supported_versions":[0,3,4.0,9]}`, `{"supported":[],"supported_versions":[4e0]}`, true, 2, false},
		{"old", `{"supported":[0,1]}`, `{"supported":[0,1]}`, false, 2, false},
		{"changed", `{"supported":[],"supported_versions":[3]}`, `{"supported":[],"supported_versions":[4]}`, true, 2, false},
		{"unknown-only", `{"supported":[],"supported_versions":[5]}`, "", false, 1, true},
		{"bad-unused-legacy", `{"supported":[false],"supported_versions":[4]}`, "", false, 1, true},
		{"missing-legacy", `{"supported_versions":[4]}`, "", false, 1, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			federation, client := testFederator(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				body, _ := io.ReadAll(r.Body)
				if r.ProtoMajor != 2 || string(body) != "[]" || r.URL.Path != "/rpc/remote.example.com/brig/api-version" || r.Header.Get("Request-Id") != "synthetic" || r.Header.Get("Wire-Origin-Domain") != "example.com" {
					t.Error("capability RPC changed")
				}
				if calls == 1 && r.Header.Get("X-Wire-API-Version") != "0" {
					t.Error("negotiation header changed")
				}
				if calls == 2 {
					wantHeader := "4"
					if test.name == "old" {
						wantHeader = "0"
					}
					if test.name == "changed" {
						wantHeader = "3"
					}
					if r.Header.Get("X-Wire-API-Version") != wantHeader {
						t.Error("explicit RPC negotiated header changed")
					}
				}
				response := test.first
				if calls > 1 {
					response = test.second
				}
				_, _ = io.WriteString(w, response)
			})
			n := &adminlessNotifier{Base: conversationJoinNotifier{Domain: "example.com"}, Federation: federation, FederationClient: client}
			got, err := n.SupportsSystemDelete(t.Context(), "remote.example.com", "synthetic")
			if got != test.want || (err != nil) != test.bad || calls != test.calls {
				t.Fatal("negotiation result changed", got, err, calls)
			}
		})
	}
	for _, value := range []string{"9223372036854775808", "4.1", "\"4\"", "1e999999999"} {
		if _, ok := adminlessVersionInteger(json.RawMessage(value)); ok {
			t.Fatal("non-Int accepted", value)
		}
	}
}

type adminlessFailQueue struct{}

func (adminlessFailQueue) Enqueue(ctx context.Context, _ string, _ payloadBundle) error {
	return ctx.Err()
}
func TestAdminlessNotificationsStopBeforeLocalOnRemoteCancellation(t *testing.T) {
	server, endpoint := brigAccountPeer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("local push after remote failure") }))
	n := &adminlessNotifier{Base: conversationJoinNotifier{Client: server.Client(), Gundeck: endpoint, Queue: adminlessFailQueue{}, Domain: "example.com"}}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	conv := &storedConversation{ID: jobUUID, RemoteMembers: []remoteConversationMember{{User: qualifiedUser{ID: jobUUID, Domain: "remote.example.com"}}}}
	if err := n.Deleted(ctx, conv, nil, "synthetic"); err == nil || strings.Contains(err.Error(), jobUUID) {
		t.Fatal("cancel/error boundary changed")
	}
}
