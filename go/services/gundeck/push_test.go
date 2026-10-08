package gundeck

import (
	"encoding/json"
	"testing"
)

func TestRecipientSplitPreservesAllClientsAndLegacyFallback(t *testing.T) {
	uid := "88888888-8888-4888-8888-888888888888"
	push := Push{Recipients: []Recipient{{User: uid, Route: "any", Clients: []string{}}}}
	clients := map[string][]wireClient{uid: {{ID: "abcd", Capabilities: []string{"consumable-notifications"}}, {ID: "ffff"}}}
	old, modern := splitRecipients(push, clients)
	if len(old) != 1 || len(old[0].Clients) != 1 || old[0].Clients[0] != "ffff" || len(modern) != 1 || len(modern[0].Clients) != 0 {
		t.Fatal("mixed client split changed")
	}
	old, modern = splitRecipients(push, nil)
	if len(old) != 1 || len(modern) != 0 || len(old[0].Clients) != 0 {
		t.Fatal("no client legacy fallback changed")
	}
}
func TestNativeEnvelopePreservesAPNSStringAndGCMPriority(t *testing.T) {
	value, err := nativeEnvelope("00000001-0000-1000-8000-000000000001", "88888888-8888-4888-8888-888888888888", "APNS_SANDBOX", "high")
	if err != nil {
		t.Fatal(err)
	}
	var outer map[string]string
	if json.Unmarshal([]byte(value), &outer) != nil {
		t.Fatal("not SNS JSON envelope")
	}
	var inner struct {
		APS map[string]any `json:"aps"`
	}
	if json.Unmarshal([]byte(outer["APNS_SANDBOX"]), &inner) != nil || inner.APS["mutable-content"] != "1" {
		t.Fatal("APNS mutable-content must remain string")
	}
	value, err = nativeEnvelope("00000001-0000-1000-8000-000000000001", "88888888-8888-4888-8888-888888888888", "GCM", "low")
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal([]byte(value), &outer)
	var gcm map[string]any
	_ = json.Unmarshal([]byte(outer["GCM"]), &gcm)
	if gcm["priority"] != "normal" {
		t.Fatal("low priority changed")
	}
}
func TestPushRejectsMalformedRecipientsAndPayload(t *testing.T) {
	for _, raw := range []string{`null`, `[{}]`, `[{"recipients":[],"payload":[]}]`, `[{"recipients":[{"user_id":"invalid","route":"any","clients":[]}],"payload":[{}]}]`, `[{"recipients":[],"payload":[1]}]`} {
		if _, err := validatePushes([]byte(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	if _, err := validatePushes([]byte(`[{"recipients":[],"payload":[{}]}]`)); err != nil {
		t.Fatal("empty recipient set is valid", err)
	}
}

func TestPayloadStorageRemovesWhitespaceWithoutRoundingLargeIntegers(t *testing.T) {
	pushes, err := validatePushes([]byte(`[{"recipients":[],"payload":[ { "seq":9007199254740993, "text":"<owned>" } ]}]`))
	if err != nil {
		t.Fatal(err)
	}
	if string(pushes[0].Payload) != `[{"seq":9007199254740993,"text":"<owned>"}]` {
		t.Fatal("payload precision/byte normalization changed", string(pushes[0].Payload))
	}
}
