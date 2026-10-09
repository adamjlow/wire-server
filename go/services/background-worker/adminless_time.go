// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"math/big"
	"strconv"
	"strings"
)

// Native UTCTimeMillis parsing accepts signed/extended Gregorian years, optional
// seconds, offsets and leap seconds. Normalize offsets in minutes first (source
// localToUTCTimeOfDay), retaining an end-of-day leap second instead of converting
// it to tomorrow. Fraction truncates to milliseconds. No time.Time narrowing.
func canonicalAdminlessMillis(value string) (string, bool) {
	if !validAdminlessAccountTime(value) {
		return "", false
	}
	m := adminlessAccountTimePattern.FindStringSubmatch(value)
	integer := func(s string) int64 { n, _ := strconv.ParseInt(s, 10, 64); return n }
	year, _ := new(big.Int).SetString(m[2], 10)
	if m[1] == "-" {
		year.Neg(year)
	}
	month, day := integer(m[3]), integer(m[4])
	if month <= 2 {
		year.Sub(year, big.NewInt(1))
	}
	era, yoe := new(big.Int), new(big.Int)
	era.DivMod(year, big.NewInt(400), yoe)
	mp := month - 3
	if month <= 2 {
		mp = month + 9
	}
	y := yoe.Int64()
	days := new(big.Int).Mul(era, big.NewInt(146097))
	days.Add(days, big.NewInt(365*y+y/4-y/100+(153*mp+2)/5+day-1-719468))
	minutes := integer(m[5])*60 + integer(m[6])
	zone := m[9]
	if zone != "Z" {
		z := strings.ReplaceAll(zone[1:], ":", "")
		offset := integer(z[:2]) * 60
		if len(z) > 2 {
			offset += integer(z[2:])
		}
		if zone[0] == '-' {
			offset = -offset
		}
		minutes -= offset
	}
	// Floor division adjusts date independently of a potentially leap second.
	dayDelta := minutes / 1440
	if minutes < 0 && minutes%1440 != 0 {
		dayDelta--
	}
	minutes -= dayDelta * 1440
	days.Add(days, big.NewInt(dayDelta))
	fraction := m[8] + "000"
	millis := integer(fraction[:3])
	second := integer(m[7])
	dayMillis := minutes*60000 + second*1000 + millis
	dayPico := new(big.Int).Mul(days, big.NewInt(86400000000000000))
	if dayMillis >= 86400000 {
		date := strings.SplitN(adminlessMillisText(dayPico), "T", 2)[0]
		return date + "T23:59:60." + fraction[:3] + "Z", true
	}
	pico := new(big.Int).Add(dayPico, new(big.Int).Mul(big.NewInt(dayMillis), big.NewInt(1000000000)))
	return adminlessMillisText(pico), true
}

// Notification ports receive canonical native output. %Y does not pad a small
// year, although the native input parser requires at least four digits. Pad only
// for internal revalidation, retaining the native unpadded output on the wire.
func canonicalAdminlessNotificationMillis(value string) (string, bool) {
	if result, ok := canonicalAdminlessMillis(value); ok {
		return result, true
	}
	sign := ""
	unsigned := value
	if strings.HasPrefix(unsigned, "-") {
		sign = "-"
		unsigned = unsigned[1:]
	}
	dash := strings.IndexByte(unsigned, '-')
	if dash < 1 || dash >= 4 {
		return "", false
	}
	padded := sign + strings.Repeat("0", 4-dash) + unsigned
	result, ok := canonicalAdminlessMillis(padded)
	return result, ok && result == value
}
