// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestScheduledReaperSQLKeepsNativeLeaseBoundsSnapshotsAndQueueGuards(t *testing.T) {
	for _, queue := range []string{"meetings", "conversations"} {
		for _, name := range []string{"refreshGroupsSQL", "selectExhaustedJobsSQL", "moveToDLQSQL", "cascadeChildrenToDLQSQL", "selectCancelledReapableJobsSQL", "deleteCancelledJobsSQL", "purgeArchiveSQL"} {
			sql, err := scheduledReaperSQL(name, queue)
			if err != nil || !strings.Contains(sql, `"arbiter"."`+queue) || strings.Contains(sql, "{{") {
				t.Fatal("native scheduled SQL rendering failed")
			}
		}
	}
	if _, err := scheduledReaperSQL("moveToDLQSQL", "meetings;DROP TABLE jobs"); err == nil {
		t.Fatal("queue identifier injection accepted")
	}
	if _, err := scheduledReaperSQL("../claim", ""); err == nil {
		t.Fatal("reaper template path traversal accepted")
	}
	for name, want := range map[string][]string{
		"selectExhaustedJobsSQL":         {"NOT suspended", "cancel_requested_at IS NULL", "COALESCE(max_attempts, 10)", "not_visible_until <= NOW()", "LIMIT 1000"},
		"selectCancelledReapableJobsSQL": {"cancel_requested_at IS NOT NULL", "not_visible_until <= NOW()", "LIMIT 1000"},
		"moveToDLQSQL":                   {"attempts = $2::integer", "rate_limit_cost", "claimed_by", "parent_state"},
		"purgeArchiveSQL":                {"archive_expires_at < NOW()", "LIMIT 10000"},
		"refreshGroupsSQL":               {"FOR"},
	} {
		if name == "refreshGroupsSQL" {
			continue
		}
		sql, _ := scheduledReaperSQL(name, "meetings")
		for _, fragment := range want {
			if !strings.Contains(sql, fragment) {
				t.Fatalf("native reaper boundary changed in %s", name)
			}
		}
	}
	sql, _ := scheduledReaperSQL("tryClaimGateSQL", "")
	if !strings.Contains(sql, "FOR UPDATE SKIP LOCKED") || !strings.Contains(sql, "last_run_at < NOW()") {
		t.Fatal("native shared gate lost mutual exclusion/interval check")
	}
}

func TestScheduledRollupSnapshotMergeUsesNativeEitherAndErrorPrecedence(t *testing.T) {
	id, dlq := int64(2), int64(7)
	message := "synthetic failure"
	rows := []reaperChildResult{
		{Source: "s", Value: json.RawMessage(`{"1":{"Right":null},"2":{"Right":"snapshot"},"3":{"Left":"snapshot-error"}}`)},
		{Source: "e", ID: &id, DLQID: &dlq, Error: &message},
		{Source: "r", ID: &id, Value: json.RawMessage(`{"nested":[1,true]}`)},
	}
	merged, err := mergeReaperResults(rows)
	if err != nil || len(merged) != 3 || string(merged["2"]) != `{"Left":"synthetic failure"}` || string(merged["1"]) != `{"Right":null}` {
		t.Fatal("native DLQ > results > snapshot precedence changed")
	}
	before := append([]reaperChildResult(nil), rows...)
	rows[1].Error = nil
	merged, err = mergeReaperResults(rows)
	if err != nil || string(merged["2"]) != `{"Left":""}` || !reflect.DeepEqual(before[0], rows[0]) {
		t.Fatal("native null DLQ error fallback or non-mutating merge changed")
	}
	for _, snapshot := range []string{`null`, `{"1":{"Right":1},"bad-key":{"Right":2}}`, `{"1":{"Left":null}}`, `{"1":{"bad":2}}`} {
		merged, err := mergeReaperResults([]reaperChildResult{{Source: "s", Value: json.RawMessage(snapshot)}, {Source: "r", ID: &id, Value: json.RawMessage(`null`)}})
		if err != nil || len(merged) != 1 || string(merged["2"]) != `{"Right":null}` {
			t.Fatal("malformed native snapshot was partly preserved")
		}
	}
	for _, invalid := range []reaperChildResult{{Source: "bad"}, {Source: "r", ID: &id}, {Source: "e", ID: &id}, {Source: "s"}} {
		if _, err := mergeReaperResults([]reaperChildResult{invalid}); err == nil {
			t.Fatal("malformed native tagged child result accepted")
		}
	}
}
