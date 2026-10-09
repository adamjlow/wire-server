// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"testing"
	"time"
)

func TestCronNativeGrammarAndMatching(t *testing.T) {
	cases := []struct {
		expression, instant string
		match               bool
	}{
		{"@YEARLY", "2026-01-01T00:00:00Z", true},
		{"@monthly", "2026-02-02T00:00:00Z", false},
		{"@weekly", "2026-10-11T00:00:00Z", true},
		{"@daily", "2026-10-09T00:00:00Z", true},
		{"@hourly", "2026-10-09T12:00:59Z", true},
		{"*/2 * * * *", "2026-10-09T12:34:00Z", true},
		{"5/10 * * * *", "2026-10-09T12:35:00Z", true},
		{"5/10 * * * *", "2026-10-09T12:34:00Z", false},
		{"1,2,20-40 * * * *", "2026-10-09T12:34:00Z", true},
		{"30 4 1,15 * fri", "2026-10-09T04:30:00Z", true}, // Both restricted: OR.
		{"30 4 1,15 * fri", "2026-10-15T04:30:00Z", true},
		{"30 4 1,15 * fri", "2026-10-08T04:30:00Z", false},
		{"0 0 */1 * fri", "2026-10-08T00:00:00Z", false},  // Star/1 unrestricted.
		{"0 0 */2 * fri", "2026-10-09T00:00:00Z", true},   // Star/2 restricted.
		{"0 0 *,15 * fri", "2026-10-08T00:00:00Z", false}, // List with star unrestricted.
		{"0 0 * jan,oct 7", "2026-10-11T00:00:00Z", true},
		{"0 0 * oct sun", "2026-10-11T00:00:00Z", true},
		{"0 0 30 feb *", "2026-02-28T00:00:00Z", false},
		{"0 0 30 feb sat", "2026-02-28T00:00:00Z", true},
	}
	for _, c := range cases {
		t.Run(c.expression+"/"+c.instant, func(t *testing.T) {
			s, err := parseScheduledCron(c.expression)
			if err != nil {
				t.Fatal(err)
			}
			tick, err := time.Parse(time.RFC3339, c.instant)
			if err != nil {
				t.Fatal(err)
			}
			if s.matches(tick) != c.match {
				t.Fatal("native match differs")
			}
		})
	}
	for _, expression := range []string{" * * * * *", "* * * * * ", "*  * * * *", "*\t* * * *", "* * * * * *", "@annually", "@midnight", "@every 1m", "60 * * * *", "* 24 * * *", "* * 0 * *", "* * * 13 *", "* * * * 8", "*/0 * * * *", "*/60 * * * *", "* */24 * * *", "* * */32 * *", "* * * */13 *", "* * * * */8", "1-0 * * * *", "1,2/3 * * * *", "1/2,3 * * * *", "+1 * * * *", "* * * january *", "* * * * monday", "* * * */jan *", "1, * * * *"} {
		if _, err := parseScheduledCron(expression); err == nil {
			t.Fatalf("invalid native expression accepted: %q", expression)
		}
	}
}
func TestCronTimezoneDedupAndMinuteTiming(t *testing.T) {
	name := "America/New_York"
	zone, err := cronTimezone(&name)
	if err != nil {
		t.Fatal(err)
	}
	first := time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC)
	second := first.Add(time.Hour)
	s, err := parseScheduledCron("30 1 * * *")
	if err != nil {
		t.Fatal(err)
	}
	if !s.matches(first.In(zone)) || !s.matches(second.In(zone)) {
		t.Fatal("DST repeated local minute not matched")
	}
	if cronDedupKey("AllowOverlap", first, zone) != cronDedupKey("AllowOverlap", second, zone) {
		t.Fatal("DST fold dedup diverged")
	}
	if cronDedupKey("SkipOverlap", first, zone) != "arbiter_cron:meetings-cleanup" {
		t.Fatal("native constant key changed")
	}
	for _, bad := range []string{"", "Local", "../UTC", "unknown/synthetic"} {
		if _, err := cronTimezone(&bad); err == nil {
			t.Fatal("invalid timezone accepted")
		}
	}
	if zone, err := cronTimezone(nil); err != nil || zone != time.UTC {
		t.Fatal("NULL timezone not UTC")
	}
	tick := first.Add(59*time.Second + 999999999*time.Nanosecond)
	if nextCronDelay(tick) != time.Microsecond || !cronMinute(tick).Equal(first) {
		t.Fatal("minute truncation/ceil delay changed")
	}
	if nextCronDelay(first) != time.Minute {
		t.Fatal("boundary must wait next minute")
	}
}
