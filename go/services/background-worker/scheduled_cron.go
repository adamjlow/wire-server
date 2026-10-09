// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"errors"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // Include IANA data in the optional static service image.
)

// Parsing/matching follows System.Cron Parser, Types and Internal.Check in
// cron0.7.2. Adapted portions retain cron_source/LICENSE (BSD-2-Clause).
// Timing and dedup follow pinned Arbiter.Worker.Cron. This is deliberately not
// a seconds/every/Quartz dialect or a scheduler with a new backfill policy.
type cronField struct {
	values     [60]bool
	restricted bool
}
type cronSchedule struct{ fields [5]cronField }

func parseScheduledCron(expression string) (cronSchedule, error) {
	var result cronSchedule
	invalid := errors.New("invalid cron schedule")
	expression = strings.ToLower(expression)
	switch expression {
	case "@yearly":
		expression = "0 0 1 1 *"
	case "@monthly":
		expression = "0 0 1 * *"
	case "@weekly":
		expression = "0 0 * * 0"
	case "@daily":
		expression = "0 0 * * *"
	case "@hourly":
		expression = "0 * * * *"
	}
	// Native classicP consumes exactly one ASCII space between fields.
	parts := strings.Split(expression, " ")
	if len(parts) != 5 {
		return result, invalid
	}
	limits := [5][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 7}}
	for index, part := range parts {
		field, err := parseCronField(part, limits[index][0], limits[index][1], index)
		if err != nil {
			return result, invalid
		}
		result.fields[index] = field
	}
	return result, nil
}
func parseCronField(text string, lo, hi, index int) (cronField, error) {
	result := cronField{restricted: true}
	invalid := errors.New("invalid cron field")
	number := func(text string) (int, error) {
		names := ""
		if index == 3 {
			names = "jan feb mar apr may jun jul aug sep oct nov dec"
		}
		if index == 4 {
			names = "mon tue wed thu fri sat sun"
		}
		for i, name := range strings.Fields(names) {
			if name == text {
				return i + 1, nil
			}
		}
		if text == "" {
			return 0, invalid
		}
		for _, c := range text {
			if c < '0' || c > '9' {
				return 0, invalid
			}
		}
		n, err := strconv.Atoi(text)
		if err != nil || n < lo || n > hi {
			return 0, invalid
		}
		return n, nil
	}
	step := 1
	pieces := strings.Split(text, "/")
	if len(pieces) > 2 {
		return result, invalid
	}
	stepped := len(pieces) == 2
	if stepped {
		// Native stepField cannot occur inside a list; its bound is this field's
		// maximum, in addition to mkStepField's strictly-positive requirement.
		if strings.Contains(pieces[0], ",") {
			return result, invalid
		}
		var err error
		step, err = number(pieces[1])
		if err != nil || step < 1 {
			return result, invalid
		}
		// Month/day names are accepted for the base, never for the step parser.
		for _, c := range pieces[1] {
			if c < '0' || c > '9' {
				return result, invalid
			}
		}
		text = pieces[0]
	}
	for _, base := range strings.Split(text, ",") {
		start, end := lo, hi
		switch {
		case base == "*":
			if !stepped || step == 1 {
				result.restricted = false
			}
		case strings.Contains(base, "-"):
			bounds := strings.Split(base, "-")
			if len(bounds) != 2 {
				return result, invalid
			}
			var err error
			start, err = number(bounds[0])
			if err != nil {
				return result, invalid
			}
			end, err = number(bounds[1])
			if err != nil || start > end {
				return result, invalid
			}
		default:
			var err error
			start, err = number(base)
			if err != nil {
				return result, invalid
			}
			if !stepped {
				end = start
			}
		}
		for n := start; n <= end; n += step {
			if index == 4 && n == 7 {
				result.values[0] = true
			} else {
				result.values[n] = true
			}
		}
	}
	return result, nil
}
func (s cronSchedule) matches(t time.Time) bool {
	dom := s.fields[2].values[t.Day()]
	dow := s.fields[4].values[int(t.Weekday())]
	day := dom && dow
	if s.fields[2].restricted && s.fields[4].restricted {
		day = dom || dow
	}
	return s.fields[0].values[t.Minute()] && s.fields[1].values[t.Hour()] && s.fields[3].values[int(t.Month())] && day
}
func cronTimezone(name *string) (*time.Location, error) {
	if name == nil {
		return time.UTC, nil
	}
	// Go's process-local special name has no native IANA-label equivalent.
	if *name == "" || *name == "Local" {
		return nil, errors.New("invalid cron timezone")
	}
	loc, err := time.LoadLocation(*name)
	if err != nil {
		return nil, errors.New("invalid cron timezone")
	}
	return loc, nil
}
func cronMinute(now time.Time) time.Time { return now.UTC().Truncate(time.Minute) }
func cronDedupKey(overlap string, tick time.Time, zone *time.Location) string {
	key := "arbiter_cron:meetings-cleanup"
	if overlap == "AllowOverlap" {
		key += ":" + tick.In(zone).Format("2006-01-02T15:04")
	}
	return key
}
func nextCronDelay(now time.Time) time.Duration {
	delay := cronMinute(now.Add(time.Minute)).Sub(now)
	if delay < 0 {
		return 0
	}
	if delay > 120*time.Second {
		return 120 * time.Second
	}
	return ((delay + time.Microsecond - 1) / time.Microsecond) * time.Microsecond
}
