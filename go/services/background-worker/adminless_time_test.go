// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"os"
	"testing"
)

func TestAdminlessReminderReusesOriginalJobGolden(t *testing.T) {
	body, err := os.ReadFile("../../../libs/wire-api/test/golden/testObject_ConversationsJobPayload_AdminlessReminder_1.json")
	if err != nil {
		t.Fatal(err)
	}
	job, err := decodeScheduledPayload("conversations", body)
	if err != nil || job.DeletionScheduledText == nil || *job.DeletionScheduledText != "2026-07-14T12:00:00.000Z" {
		t.Fatal("original reminder timestamp changed", err)
	}
}
func TestAdminlessTimestampNativeExtendedOffsetsAndLeapSeconds(t *testing.T) {
	cases := map[string]string{
		"2026-01-01 00:30+01":                    "2025-12-31T23:30:00.000Z",
		"2026-12-31T23:30:00.1239999999999-0130": "2027-01-01T01:00:00.123Z",
		"2000-02-29T23:59:60.123999Z":            "2000-02-29T23:59:60.123Z",
		"2000-02-29T12:59:60Z":                   "2000-02-29T13:00:00.000Z",
		"2000-03-01T00:59:60+01":                 "2000-02-29T23:59:60.000Z",
		"400000000001970-01-01T00:00:00.000Z":    "400000000001970-01-01T00:00:00.000Z",
		"0001-01-01T00:00Z":                      "1-01-01T00:00:00.000Z",
		"-0001-01-01T00:00Z":                     "-1-01-01T00:00:00.000Z",
	}
	for input, want := range cases {
		got, ok := canonicalAdminlessMillis(input)
		if !ok || got != want {
			t.Fatalf("timestamp got%s want%s", got, want)
		}
		if normalized, ok := canonicalAdminlessNotificationMillis(got); !ok || normalized != got {
			t.Fatal("native output did not survive notification port")
		}
	}
	for _, input := range []string{"2026-01-01T00:00 Z", "1900-02-29T00:00Z", "2026-01-01T00:00:61Z", "1-01-01T00:00Z"} {
		if _, ok := canonicalAdminlessMillis(input); ok {
			t.Fatal("native-invalid timestamp accepted", input)
		}
	}
}
