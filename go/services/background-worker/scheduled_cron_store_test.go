// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import "testing"

func TestScheduledCronOverridesPreserveEmptyAndFallback(t *testing.T) {
	empty, utc, invalid := "", "UTC", "invalid"
	r := scheduledCronRow{DefaultExpression: "0 * * * *", DefaultOverlap: "AllowOverlap", DefaultTimezone: &utc}
	if r.effectiveExpression() != r.DefaultExpression || r.effectiveTimezone() != &utc || r.effectiveOverlap("SkipOverlap") != "AllowOverlap" {
		t.Fatal("defaults lost")
	}
	r.OverrideExpression = &empty
	r.OverrideTimezone = &empty
	r.OverrideOverlap = &invalid
	if r.effectiveExpression() != "" || r.effectiveTimezone() != &empty || r.effectiveOverlap("SkipOverlap") != "SkipOverlap" {
		t.Fatal("empty override or native policy fallback lost")
	}
	r.OverrideTimezone = &utc
	if *r.effectiveTimezone() != "UTC" {
		t.Fatal("explicit UTC override lost")
	}
}
