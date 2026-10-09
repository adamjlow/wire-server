// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"fmt"
	"testing"
)

func TestScheduledPayloadUsesOriginalQueueEnvelope(t *testing.T) {
	for _, tag := range []string{"adminless_setup", "adminless_deletion", "adminless_reminder"} {
		body := fmt.Sprintf(`{"type":%q,"data":{"team_id":%q,"conversation_id":%q,"orig_user_id":null,"request_id":"","deletion_scheduled_for":"2026-10-09T12:13:14.123Z","extra":true}}`, tag, jobUUID, jobUUID)
		payload, err := decodeScheduledPayload("conversations", []byte(body))
		if err != nil || payload.Team != jobUUID || payload.Type != tag || payload.Actor != nil || payload.RequestID != "" {
			t.Fatalf("native %s envelope rejected: %v", tag, err)
		}
		if tag == "adminless_reminder" && (payload.DeletionScheduledFor == nil || payload.DeletionScheduledFor.Nanosecond() != 123000000) {
			t.Fatal("native reminder deletion time lost")
		}
	}
	if _, err := decodeScheduledPayload("conversations", []byte(fmt.Sprintf(`{"type":"adminless_setup","data":{"team_id":%q,"request_id":"synthetic","conversation_id":123,"deletion_scheduled_for":false}}`, jobUUID))); err != nil {
		t.Fatal("extra fields from other tags stopped being ignored")
	}
	if _, err := decodeScheduledPayload("meetings", []byte(`{"type":"meetings_cleanup","data":{"extra":true}}`)); err != nil {
		t.Fatal("native empty object schema changed")
	}
}

func TestScheduledPayloadRejectsMissingAndMalformedFieldsAndWrongQueue(t *testing.T) {
	for _, test := range []struct{ queue, body string }{
		{"meetings", `{"type":"adminless_setup","data":{}}`},
		{"conversations", `{"type":"meetings_cleanup","data":{}}`},
		{"meetings", `{"type":"meetings_cleanup","data":null}`},
		{"meetings", `{"type":"meetings_cleanup","data":[]}`},
		{"conversations", `{"type":"adminless_setup","data":{"team_id":"x;SELECT","request_id":"synthetic"}}`},
		{"conversations", `{"type":"adminless_setup","data":{}}`},
		{"conversations", fmt.Sprintf(`{"type":"adminless_deletion","data":{"team_id":%q,"request_id":"synthetic"}}`, jobUUID)},
		{"conversations", fmt.Sprintf(`{"type":"adminless_reminder","data":{"team_id":%q,"conversation_id":%q,"request_id":"synthetic","deletion_scheduled_for":"invalid"}}`, jobUUID, jobUUID)},
		{"conversations", fmt.Sprintf(`{"type":"adminless_setup","data":{"team_id":%q,"request_id":"synthetic","orig_user_id":"invalid"}}`, jobUUID)},
	} {
		if _, err := decodeScheduledPayload(test.queue, []byte(test.body)); err == nil {
			t.Fatal("malformed or foreign scheduled payload accepted")
		}
	}
}
