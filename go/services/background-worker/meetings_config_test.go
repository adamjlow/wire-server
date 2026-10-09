// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"math"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
)

func TestMeetingsConfigRequiredTypesAndNativeCronSerialization(t *testing.T) {
	for _, c := range []struct{ schedule, want string }{{"@daily", "0 0 * * *"}, {"00 01 01,02 Jan-Feb Mon-Sun", "0 1 1,2 1-2 1-7"}, {"05/010 * * * *", "5/10 * * * *"}} {
		var config MeetingsCleanupConfig
		body := "cleanOlderThanHours: 0\nbatchSize: 1\nschedule: '" + c.schedule + "'\n"
		if err := yaml.Unmarshal([]byte(body), &config); err != nil || config.Schedule != c.want {
			t.Fatalf("source serialization %+v error%v", config, err)
		}
	}
	for _, body := range []string{
		"batchSize: 1\nschedule: '@daily'", "cleanOlderThanHours: 1\nschedule: '@daily'", "cleanOlderThanHours: 1\nbatchSize: 1",
		"cleanOlderThanHours: null\nbatchSize: 1\nschedule: '@daily'", "cleanOlderThanHours: -1\nbatchSize: 1\nschedule: '@daily'",
		"cleanOlderThanHours: 0\nbatchSize: 0\nschedule: '@daily'", "cleanOlderThanHours: 0\nbatchSize: 1.5\nschedule: '@daily'",
		"cleanOlderThanHours: '1'\nbatchSize: 1\nschedule: '@daily'", "cleanOlderThanHours: 0\nbatchSize: '1'\nschedule: '@daily'",
		"cleanOlderThanHours: .nan\nbatchSize: 1\nschedule: '@daily'", "cleanOlderThanHours: .inf\nbatchSize: 1\nschedule: '@daily'",
		"cleanOlderThanHours: 0\nbatchSize: 1\nschedule: 1", "cleanOlderThanHours: 0\nbatchSize: 1\nschedule: '*  * * * *'",
	} {
		var c MeetingsCleanupConfig
		if yaml.Unmarshal([]byte(body), &c) == nil {
			t.Fatalf("invalid configuration accepted: %q", body)
		}
	}
}
func TestMeetingCutoffNativeFixedPrecisionAndLongRetention(t *testing.T) {
	now := time.Date(2026, 10, 9, 0, 0, 0, 123456789, time.UTC)
	for _, c := range []struct {
		hours float64
		want  string
	}{
		{0, "2026-10-09T00:00:00.123456Z"},
		{0.0014, "2026-10-08T23:59:55.083456Z"},
		{0.1, "2026-10-08T23:54:00.123456Z"},
		{8766000, "1026-10-02T00:00:00.123456Z"},
		{1e300, "2026-10-09T00:00:00.123456Z"}, // Native Int64 timestamp wrap.
	} {
		got, err := meetingRetentionCutoff(now, c.hours)
		if err != nil || got.Format(time.RFC3339Nano) != c.want {
			t.Fatalf("hours%g cutoff%v error%v want%s", c.hours, got, err, c.want)
		}
	}
	for _, hours := range []float64{-1, math.NaN(), math.Inf(1)} {
		if _, err := meetingRetentionCutoff(now, hours); err == nil {
			t.Fatal("invalid retention accepted")
		}
	}
	f := &cleanupFixture{}
	config := MeetingsCleanupConfig{RetentionHours: 0.0014, BatchSize: 2, Schedule: "* * * * *"}
	result, err := runConfiguredMeetingsCleanup(t.Context(), config, f, f, now, nil)
	want, _ := meetingRetentionCutoff(now, config.RetentionHours)
	if err != nil || !result.Completed || len(f.cutoffs) != 1 || !f.cutoffs[0].Equal(want) {
		t.Fatal("configured cleanup cutoff not used")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if result, err := runConfiguredMeetingsCleanup(ctx, config, f, f, now, nil); err == nil || result.Completed {
		t.Fatal("cancelled configured run completed")
	}
}
