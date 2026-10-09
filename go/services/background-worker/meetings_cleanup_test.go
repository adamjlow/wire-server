// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

type cleanupFixture struct {
	pages   [][]storedMeeting
	calls   []string
	conv    *storedConversation
	fail    string
	cutoffs []time.Time
}

func (f *cleanupFixture) OldMeetings(_ context.Context, t time.Time, _ int) ([]storedMeeting, error) {
	f.calls = append(f.calls, "read")
	f.cutoffs = append(f.cutoffs, t)
	if f.fail == "read" {
		return nil, errors.New("synthetic failure")
	}
	if len(f.pages) == 0 {
		return nil, nil
	}
	p := f.pages[0]
	f.pages = f.pages[1:]
	return p, nil
}
func (f *cleanupFixture) Conversation(context.Context, string) (*storedConversation, error) {
	f.calls = append(f.calls, "conversation")
	if f.fail == "conversation" {
		return nil, errors.New("synthetic failure")
	}
	return f.conv, nil
}
func (f *cleanupFixture) DeleteLocalConversationUnchecked(context.Context, string) error {
	return f.effect("unchecked")
}
func (f *cleanupFixture) DeleteMeetingCode(context.Context, string) error { return f.effect("code") }
func (f *cleanupFixture) DeleteMeeting(context.Context, string) error     { return f.effect("meeting") }
func (f *cleanupFixture) effect(name string) error {
	f.calls = append(f.calls, name)
	if f.fail == name {
		return errors.New("synthetic failure")
	}
	return nil
}
func syntheticMeeting(t *testing.T) storedMeeting {
	t.Helper()
	id, err := postgresUUID("00000000-0000-0000-0000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	return storedMeeting{ID: id, Creator: id, Conversation: id, Title: "synthetic", TZ: "UTC", Type: "scheduled", End: time.Unix(1000, 0)}
}
func TestMeetingCleanupNativeEffectsAndErrorBoundaries(t *testing.T) {
	meeting := syntheticMeeting(t)
	kind := int32(2)
	cutoff := time.Unix(2000, 0)
	for _, c := range []struct {
		name string
		conv *storedConversation
		fail string
		want []string
	}{
		{"meeting", &storedConversation{ID: postgresUUIDString(meeting.Conversation), Metadata: map[string]any{"group_conv_type": &kind}}, "", []string{"read", "conversation", "unchecked", "code", "meeting"}},
		{"missing", nil, "", []string{"read", "conversation", "code", "meeting"}},
		{"ordinary", &storedConversation{ID: postgresUUIDString(meeting.Conversation)}, "", []string{"read", "conversation", "code", "meeting"}},
		{"mismatch", &storedConversation{ID: "different", Metadata: map[string]any{"group_conv_type": &kind}}, "", []string{"read", "conversation", "code", "meeting"}},
		{"lookup-failure", nil, "conversation", []string{"read", "conversation"}},
		{"delete-failure", &storedConversation{ID: postgresUUIDString(meeting.Conversation), Metadata: map[string]any{"group_conv_type": &kind}}, "unchecked", []string{"read", "conversation", "unchecked"}},
		{"code-failure", nil, "code", []string{"read", "conversation", "code"}},
		{"row-failure", nil, "meeting", []string{"read", "conversation", "code", "meeting"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := &cleanupFixture{pages: [][]storedMeeting{{meeting}}, conv: c.conv, fail: c.fail}
			n, err := cleanupMeetingsBatch(t.Context(), f, f, cutoff, 10)
			if !reflect.DeepEqual(f.calls, c.want) || (err != nil) != (c.fail != "") || c.fail != "" && n != 0 || c.fail == "" && n != 1 {
				t.Fatalf("effects %v count%d error%v", f.calls, n, err)
			}
		})
	}
	f := &cleanupFixture{pages: [][]storedMeeting{{meeting, meeting}, {meeting}}}
	result, err := runMeetingsCleanup(t.Context(), f, f, cutoff, 2, nil)
	if err != nil || !result.Completed || result.TotalDeleted != 3 || len(f.cutoffs) != 2 || !f.cutoffs[0].Equal(f.cutoffs[1]) {
		t.Fatal("batch loop/cutoff changed")
	}
	f = &cleanupFixture{pages: [][]storedMeeting{{meeting}}, fail: "meeting"}
	result, err = runMeetingsCleanup(t.Context(), f, f, cutoff, 2, nil)
	if err != nil || !result.Completed || result.BatchError == nil || result.TotalDeleted != 0 {
		t.Fatal("native caught failure/run counter boundary changed")
	}
	f = &cleanupFixture{}
	result, err = runMeetingsCleanup(t.Context(), f, f, cutoff, 0, nil)
	if err == nil || result.Completed || len(f.calls) != 0 {
		t.Fatal("invalid batch ran effects or incremented runs")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result, err = runMeetingsCleanup(ctx, f, f, cutoff, 2, nil)
	if !errors.Is(err, context.Canceled) || result.Completed || len(f.calls) != 0 {
		t.Fatal("cancelled run performed effects")
	}
}
func TestStoredMeetingValidationAndStableMerge(t *testing.T) {
	valid := syntheticMeeting(t)
	if valid.validate() != nil {
		t.Fatal("valid stored meeting rejected")
	}
	// Native recurrence decoder leaves unrelated interval/until fields alone
	// when frequency is NULL; signed intervals are not API-validated again.
	interval := int32(-1)
	freq := "daily"
	until := valid.End.Add(time.Hour)
	recurring := valid
	recurring.Frequency = &freq
	recurring.Interval = &interval
	recurring.Until = &until
	if recurring.validate() != nil || !recurring.effectiveEnd().Equal(until) {
		t.Fatal("bounded recurrence decoding changed")
	}
	recurring.Until = nil
	if recurring.effectiveEnd() != nil {
		t.Fatal("open recurrence expired")
	}
	for _, change := range []func(*storedMeeting){func(m *storedMeeting) { m.ID = pgtype.UUID{} }, func(m *storedMeeting) { m.Title = "" }, func(m *storedMeeting) { m.Title = strings.Repeat("x", 257) }, func(m *storedMeeting) { m.TZ = "Local" }, func(m *storedMeeting) { m.Type = "invalid" }, func(m *storedMeeting) { m.Frequency = &freq }} {
		m := valid
		change(&m)
		if m.validate() == nil {
			t.Fatal("invalid full row accepted")
		}
	}
	a, b, c := valid, valid, valid
	a.Title = "a"
	b.Title = "b"
	c.Title = "c"
	c.End = c.End.Add(-time.Minute)
	merged := mergeExpiredMeetings([]storedMeeting{a}, []storedMeeting{c, b}, 2)
	if len(merged) != 2 || merged[0].Title != "c" || merged[1].Title != "a" {
		t.Fatal("effective end stable merge/cap changed")
	}
	key, err := meetingCodeKey("00000000-0000-0000-0000-000000000001")
	if err != nil || key != "b1Zp3nLCnk7jzQzl07rd" {
		t.Fatalf("meeting code key %s %v", key, err)
	}
	if _, err := meetingCodeKey("invalid"); err == nil {
		t.Fatal("invalid code identifier accepted")
	}
}

func TestMeetingCodeStorageRouting(t *testing.T) {
	// No pool/connection may be touched in source Cassandra-only code mode.
	cass := routedMeetingsCleanupStore{Codes: storageCassandra}
	if err := cass.DeleteMeetingCode(t.Context(), "unused"); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []storageLocation{storageMigrating, storagePostgres, "invalid"} {
		if err := (routedMeetingsCleanupStore{Codes: mode}).DeleteMeetingCode(t.Context(), "unused"); err == nil {
			t.Fatal("missing/invalid code store silently ignored")
		}
	}
}
