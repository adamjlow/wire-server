// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"errors"
	"math/big"
	"reflect"
	"testing"
	"time"
)

type adminlessWriteFixture struct {
	Jobs []adminlessScheduledWrite
	Fail int
}

func (f *adminlessWriteFixture) InsertAdminlessJob(_ context.Context, job adminlessScheduledWrite) error {
	if f.Fail > 0 && len(f.Jobs)+1 == f.Fail {
		return errors.New("synthetic insertion failed")
	}
	f.Jobs = append(f.Jobs, job)
	return nil
}
func TestAdminlessSchedulingSourceOrderTimingAndPayload(t *testing.T) {
	now := time.Date(2026, 10, 9, 10, 11, 12, 123456789, time.UTC)
	f := &adminlessWriteFixture{}
	scheduler := adminlessDurableScheduler{Store: f, Now: func() time.Time { return now }}
	feature := adminlessFeature{DeletionPico: big.NewInt(10000000000000), ReminderPico: []*big.Int{big.NewInt(2000000000000), big.NewInt(10000000000000), big.NewInt(11000000000000), big.NewInt(500000000000), big.NewInt(2000000000000)}}
	conv := &storedConversation{ID: jobUUID}
	actor := jobUUID
	if err := scheduler.Schedule(t.Context(), jobUUID, conv, &actor, "synthetic", feature); err != nil {
		t.Fatal(err)
	}
	if len(f.Jobs) != 4 {
		t.Fatal("strict-before reminders or duplicate insertion order changed")
	}
	expected := []time.Time{now.Add(10 * time.Second), now.Add(8 * time.Second), now.Add(9500 * time.Millisecond), now.Add(8 * time.Second)}
	keys := []string{"adminless-deletion:" + jobUUID, "adminless-reminder:" + jobUUID + ":2s", "adminless-reminder:" + jobUUID + ":0.5s", "adminless-reminder:" + jobUUID + ":2s"}
	for i, job := range f.Jobs {
		if !job.Visible.Equal(expected[i].Truncate(time.Microsecond)) || job.Dedup != keys[i] {
			t.Fatal("visibility/dedup changed")
		}
		payload, err := decodeScheduledPayload("conversations", job.Payload)
		if err != nil || payload.Team != jobUUID || payload.Conversation != jobUUID || payload.Actor == nil || *payload.Actor != actor || payload.RequestID != "synthetic" {
			t.Fatal("native payload changed")
		}
		if i > 0 && (payload.DeletionScheduledFor == nil || !payload.DeletionScheduledFor.Equal(now.Add(10*time.Second).Truncate(time.Millisecond))) {
			t.Fatal("reminder deletion timestamp changed")
		}
	}
	f.Jobs = nil
	f.Fail = 3
	if err := scheduler.Schedule(t.Context(), jobUUID, conv, nil, "synthetic", feature); err == nil || len(f.Jobs) != 2 {
		t.Fatal("separate insert failure boundary changed")
	}
}
func TestAdminlessSchedulingFixedPicoAndExtendedYears(t *testing.T) {
	for _, text := range []string{"1970-01-01T00:00:00.000Z", "2000-02-29T23:59:59.999Z", "1900-03-01T01:02:03.004Z", "2026-10-09T10:11:12.123Z"} {
		v, err := time.Parse(time.RFC3339Nano, text)
		if err != nil {
			t.Fatal(err)
		}
		pico := new(big.Int).Mul(big.NewInt(v.Unix()), big.NewInt(1000000000000))
		pico.Add(pico, big.NewInt(int64(v.Nanosecond())*1000+999))
		if got := adminlessMillisText(pico); got != text {
			t.Fatalf("calendar/floor got%s want%s", got, text)
		}
		if !adminlessPostgresTime(pico).Equal(v) {
			t.Fatal("microsecond floor changed")
		}
	}
	// Native accepted Word64 days cannot fit Duration or Unix seconds. Exact
	// 400-year Gregorian cycles provide an independent calendar expectation.
	days := new(big.Int).Mul(big.NewInt(146097), big.NewInt(1000000000000))
	pico := new(big.Int).Mul(days, big.NewInt(86400000000000000))
	if got := adminlessMillisText(pico); got != "400000000001970-01-01T00:00:00.000Z" {
		t.Fatal("extended Gregorian cycles overflow", got)
	}
	if !reflect.DeepEqual([]string{adminlessNominalText(big.NewInt(0)), adminlessNominalText(big.NewInt(1)), adminlessNominalText(big.NewInt(1230000000000))}, []string{"0s", "0.000000000001s", "1.23s"}) {
		t.Fatal("NominalDiffTime dedup rendering changed")
	}
	f := &adminlessWriteFixture{}
	scheduler := adminlessDurableScheduler{Store: f, Now: func() time.Time { return time.Unix(0, 0) }}
	if err := scheduler.Schedule(t.Context(), jobUUID, &storedConversation{ID: jobUUID}, nil, "synthetic", adminlessFeature{DeletionPico: pico, ReminderPico: []*big.Int{big.NewInt(1)}}); err != nil || len(f.Jobs) != 2 {
		t.Fatal("large accepted duration rejected", err)
	}
}
