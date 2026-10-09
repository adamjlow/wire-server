// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"
)

type adminlessScheduledWrite struct {
	Payload      []byte
	Group, Dedup string
	Visible      time.Time
}
type adminlessJobWriter interface {
	InsertAdminlessJob(context.Context, adminlessScheduledWrite) error
}
type adminlessDurableScheduler struct {
	Store adminlessJobWriter
	Now   func() time.Time
}

// Source gets Now once, inserts deletion first, then each strictly earlier
// reminder in configured order. Each insert commits independently; IgnoreDuplicate
// does not replace existing payload/visibility after a retry or feature change.
func (s adminlessDurableScheduler) Schedule(ctx context.Context, team string, conv *storedConversation, actor *string, requestID string, feature adminlessFeature) error {
	if s.Store == nil || s.Now == nil || conv == nil || feature.DeletionPico == nil || feature.DeletionPico.Sign() < 0 {
		return errors.New("adminless scheduler unavailable")
	}
	tid, err := parseUserID(team)
	if err != nil {
		return errors.New("invalid adminless scheduling team")
	}
	cid, err := parseUserID(conv.ID)
	if err != nil {
		return errors.New("invalid adminless scheduling conversation")
	}
	var origin *string
	if actor != nil {
		id, err := parseUserID(*actor)
		if err != nil {
			return errors.New("invalid adminless scheduling actor")
		}
		origin = stringPtr(id.String())
	}
	for _, reminder := range feature.ReminderPico {
		if reminder == nil || reminder.Sign() < 0 {
			return errors.New("invalid adminless reminder timeout")
		}
	}
	now := s.Now().UTC()
	nowPico := new(big.Int).Mul(big.NewInt(now.Unix()), big.NewInt(1000000000000))
	nowPico.Add(nowPico, big.NewInt(int64(now.Nanosecond())*1000))
	deletion := new(big.Int).Add(nowPico, feature.DeletionPico)
	data := map[string]any{"team_id": tid.String(), "conversation_id": cid.String(), "request_id": requestID}
	if origin != nil {
		data["orig_user_id"] = *origin
	}
	insert := func(kind string, visible *big.Int, suffix string) error {
		payload, err := json.Marshal(map[string]any{"type": "adminless_" + kind, "data": data})
		if err != nil {
			return errors.New("adminless job encoding failed")
		}
		group := "adminless-" + kind + ":" + cid.String()
		return s.Store.InsertAdminlessJob(ctx, adminlessScheduledWrite{Payload: payload, Group: group, Dedup: group + suffix, Visible: adminlessPostgresTime(visible)})
	}
	if err := insert("deletion", deletion, ""); err != nil {
		return err
	}
	data["deletion_scheduled_for"] = adminlessMillisText(deletion)
	for _, reminder := range feature.ReminderPico {
		if reminder.Cmp(feature.DeletionPico) >= 0 {
			continue
		}
		visible := new(big.Int).Sub(deletion, reminder)
		if err := insert("reminder", visible, ":"+adminlessNominalText(reminder)); err != nil {
			return err
		}
	}
	return nil
}

// PostgreSQL.Binary.Time encodes floored microseconds from epoch2000 as Int64.
// Preserve the native wrap rather than impose a new timeout policy.
func adminlessPostgresTime(unixPico *big.Int) time.Time {
	sinceEpoch := new(big.Int).Sub(unixPico, new(big.Int).Mul(big.NewInt(946684800), big.NewInt(1000000000000)))
	micro := new(big.Int).Div(sinceEpoch, big.NewInt(1000000))
	modulus := new(big.Int).Lsh(big.NewInt(1), 64)
	wrapped := new(big.Int).Mod(micro, modulus)
	if wrapped.Bit(63) != 0 {
		wrapped.Sub(wrapped, modulus)
	}
	v := wrapped.Int64()
	return time.Unix(v/1000000+946684800, (v%1000000)*1000).UTC()
}
func adminlessNominalText(pico *big.Int) string {
	seconds, fraction := new(big.Int), new(big.Int)
	seconds.QuoRem(pico, big.NewInt(1000000000000), fraction)
	if fraction.Sign() == 0 {
		return seconds.String() + "s"
	}
	return seconds.String() + "." + strings.TrimRight(fmt.Sprintf("%012d", fraction.Int64()), "0") + "s"
}

// The JSON deletion timestamp is the unwrapped UTC value floored to milliseconds,
// even if the separate PostgreSQL visibility encoding wraps. Gregorian cycles
// keep all source-accepted Word64-day timeouts representable without Go Unix or
// Duration overflow. Only the bounded day-of-era and time-of-day use int64.
func adminlessMillisText(unixPico *big.Int) string {
	days, dayPico := new(big.Int), new(big.Int)
	days.DivMod(unixPico, big.NewInt(86400000000000000), dayPico)
	z := new(big.Int).Add(days, big.NewInt(719468))
	era, dayOfEra := new(big.Int), new(big.Int)
	era.DivMod(z, big.NewInt(146097), dayOfEra)
	doe := dayOfEra.Int64()
	yoe := (doe - doe/1460 + doe/36524 - doe/146096) / 365
	year := new(big.Int).Add(new(big.Int).Mul(era, big.NewInt(400)), big.NewInt(yoe))
	doy := doe - (365*yoe + yoe/4 - yoe/100)
	mp := (5*doy + 2) / 153
	day := doy - (153*mp+2)/5 + 1
	month := mp + 3
	if mp >= 10 {
		month = mp - 9
	}
	if month <= 2 {
		year.Add(year, big.NewInt(1))
	}
	ms := new(big.Int).Div(dayPico, big.NewInt(1000000000)).Int64()
	// Native formatTime %Y has no padding (including signed/extended years).
	yearText := year.String()
	return fmt.Sprintf("%s-%02d-%02dT%02d:%02d:%02d.%03dZ", yearText, month, day, ms/3600000, (ms/60000)%60, (ms/1000)%60, ms%1000)
}
