// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func deletionNativeEvent(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("../../../libs/wire-api/test/golden", name))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestDeletionEventOriginalNativeGoldens(t *testing.T) {
	// All14 existing event goldens are reused without modifying their bytes.
	files, err := filepath.Glob("../../../libs/wire-api/test/golden/testObject_Event_conversation*.json")
	if err != nil || len(files) != 14 {
		t.Fatal("native event fixture inventory changed", len(files), err)
	}
	for _, path := range files {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !validDeletionConversationEvent(raw) {
			t.Fatal("native event rejected", filepath.Base(path))
		}
	}
	// Native conversation goldens use V9; explicitly serialize V2 access roles
	// and mandatory legacy MLS fields as the native Event schema requires.
	for _, name := range []string{"testObject_Conversation_user_1.json", "testObject_Conversation_user_3.json", "testObject_Conversation_user_4.json"} {
		var conversation map[string]any
		if json.Unmarshal(deletionNativeEvent(t, name), &conversation) != nil {
			t.Fatal("conversation fixture")
		}
		conversation["access_role_v2"] = conversation["access_role"]
		conversation["access_role"] = "non_activated"
		if conversation["protocol"] == "mls" {
			if _, found := conversation["epoch_timestamp"]; !found {
				conversation["epoch_timestamp"] = nil
			}
			if _, found := conversation["cipher_suite"]; !found {
				conversation["cipher_suite"] = 1
			}
		}
		var event map[string]any
		_ = json.Unmarshal(deletionNativeEvent(t, "testObject_Event_conversation_9.json"), &event)
		event["type"] = "conversation.create"
		event["data"] = conversation
		raw, _ := json.Marshal(event)
		if !validDeletionConversationEvent(raw) {
			t.Fatal("native V2 conversation projection rejected", name)
		}
	}
}

func TestDeletionEventSecurityAndExactNumbers(t *testing.T) {
	base := deletionNativeEvent(t, "testObject_Event_conversation_9.json")
	for _, field := range []string{"qualified_conversation", "qualified_from", "via", "time", "type", "data"} {
		var event map[string]any
		_ = json.Unmarshal(base, &event)
		delete(event, field)
		raw, _ := json.Marshal(event)
		if validDeletionConversationEvent(raw) {
			t.Fatal("missing required native field accepted", field)
		}
	}
	for _, payload := range []any{nil, map[string]any{"reason": "removed", "qualified_user_ids": []any{}}, map[string]any{"reason": "unknown", "qualified_user_ids": []any{}, "user_ids": []any{}}, map[string]any{"reason": "removed", "qualified_user_ids": nil, "user_ids": []any{}}} {
		var event map[string]any
		_ = json.Unmarshal(base, &event)
		event["data"] = payload
		raw, _ := json.Marshal(event)
		if validDeletionConversationEvent(raw) {
			t.Fatal("malformed member-leave data accepted")
		}
	}
	for _, test := range []struct {
		text          string
		bits          int
		signed, valid bool
	}{{"18446744073709551615.0", 64, false, true}, {"18446744073709551616", 64, false, false}, {"-0.0", 64, false, true}, {"1.00000000000000000001", 64, false, false}, {"2147483647e0", 32, true, true}, {"2147483648", 32, true, false}, {"-2147483648.0", 32, true, true}, {"1e1000000", 64, false, false}, {"0e1000000", 64, false, true}} {
		if deletionEventInteger(test.bits, test.signed)(json.RawMessage(test.text)) != test.valid {
			t.Fatal("native exact integer boundary", test.text)
		}
	}
	for _, raw := range []json.RawMessage{[]byte(`"AR=="`), []byte(`"AQ==\n"`), []byte(`"AQ"`)} {
		if deletionEventBase64(raw) {
			t.Fatal("noncanonical native base64 admitted")
		}
	}
}

func TestDeletionGalleyRPCAdmissionAndEffects(t *testing.T) {
	for _, test := range []struct {
		status int
		body   string
		valid  bool
	}{{200, string(deletionNativeEvent(t, "testObject_Event_conversation_9.json")), true}, {200, "", false}, {200, "{}", false}, {404, "invalid ignored body", true}, {204, "", false}, {202, "", false}} {
		t.Run(http.StatusText(test.status)+test.body[:min(len(test.body), 2)], func(t *testing.T) {
			bot := deletionServiceBot{user: gocql.TimeUUID(), conversation: gocql.TimeUUID()}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]string
				if r.Method != http.MethodDelete || r.URL.Path != "/i/bots" || r.Header.Get("Z-User") != bot.user.String() || r.Header.Get("Z-Connection") != "" || r.Header.Get("Content-Type") != "application/json" || json.NewDecoder(r.Body).Decode(&body) != nil || body["conversation"] != bot.conversation.String() || body["bot"] != bot.user.String() {
					t.Error("native bot deletion RPC contract")
				}
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			peers := brigPeers{client: server.Client(), galley: deletionPeerEndpoint(server)}
			if (peers.removeServiceBotMember(context.Background(), bot) == nil) != test.valid {
				t.Fatal("native peer admission")
			}
		})
	}
	provider, service := gocql.TimeUUID(), gocql.TimeUUID()
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		var body map[string]string
		if r.Method != http.MethodDelete || r.URL.Path != "/i/services" || r.Header.Get("Z-User") != "" || json.NewDecoder(r.Body).Decode(&body) != nil || body["id"] != service.String() || body["provider"] != provider.String() {
			t.Error("native service connection RPC")
		}
		if attempts == 1 {
			w.WriteHeader(503)
		} else {
			w.WriteHeader(202)
		}
	}))
	defer server.Close()
	if err := (brigPeers{client: server.Client(), galley: deletionPeerEndpoint(server)}).deleteServiceConnection(context.Background(), provider, service); err != nil || attempts != 2 {
		t.Fatal("native 5xx retry/any2xx service admission", attempts, err)
	}
}
