// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"encoding/binary"
	"errors"
	"reflect"
	"testing"
)

type removalFixture struct {
	conv  *storedConversation
	subs  []removalSubConversation
	calls []string
	fail  string
	team  *string
}

func (f *removalFixture) effect(name string) error {
	f.calls = append(f.calls, name)
	if f.fail == name {
		return errors.New("synthetic failure")
	}
	return nil
}
func (f *removalFixture) Conversation(context.Context, string) (*storedConversation, error) {
	err := f.effect("read")
	return f.conv, err
}
func (f *removalFixture) UpsertLocalMembers(context.Context, string, []string, string) error {
	panic("unexpected member write")
}
func (f *removalFixture) SubConversations(context.Context, string) ([]removalSubConversation, error) {
	err := f.effect("subs")
	return append([]removalSubConversation(nil), f.subs...), err
}
func (f *removalFixture) DeleteSubConversation(_ context.Context, _ string, id string) error {
	return f.effect("sub:" + id)
}
func (f *removalFixture) RemoveMLSClients(_ context.Context, g []byte) error {
	return f.effect("clients:" + string(g))
}
func (f *removalFixture) RemoveHistoryClients(_ context.Context, g []byte) error {
	return f.effect("history:" + string(g))
}
func (f *removalFixture) DeleteProposals(_ context.Context, g []byte) error {
	return f.effect("proposals:" + string(g))
}
func (f *removalFixture) DeleteConversationCode(context.Context, string) error {
	return f.effect("code")
}
func (f *removalFixture) DeleteConversationState(_ context.Context, _ string, team *string) error {
	f.team = team
	return f.effect("state")
}

func TestUncheckedRemovalSourceOrderAndFailureBoundaries(t *testing.T) {
	team := "00000000-0000-0000-0000-000000000002"
	order := []string{"read", "subs", "sub:a", "clients:a", "history:a", "proposals:a", "sub:z", "clients:z", "history:z", "proposals:z", "clients:main", "history:main", "proposals:main", "code", "state"}
	for _, protocol := range []int32{0, 1, 2} {
		want := order
		if protocol == 0 {
			want = []string{"read", "code", "state"}
		}
		for failure := -1; failure < len(want); failure++ {
			f := &removalFixture{conv: &storedConversation{ID: "00000000-0000-0000-0000-000000000001", Team: &team, Metadata: map[string]any{"protocol": protocol, "group_id": []byte("main")}}, subs: []removalSubConversation{{ID: "z", Group: []byte("z")}, {ID: "a", Group: []byte("a")}}}
			expected := want
			if failure >= 0 {
				f.fail = want[failure]
				expected = want[:failure+1]
			}
			err := (scheduledConversationRemoval{Read: f, State: f}).DeleteLocalConversationUnchecked(context.Background(), f.conv.ID)
			if (err != nil) != (failure >= 0) || !reflect.DeepEqual(f.calls, expected) {
				t.Fatalf("protocol=%d failure=%d calls=%v error=%v", protocol, failure, f.calls, err)
			}
			if failure == -1 && f.team != &team {
				t.Fatal("team identity lost")
			}
		}
	}
	f := &removalFixture{}
	if err := (scheduledConversationRemoval{Read: f, State: f}).DeleteLocalConversationUnchecked(context.Background(), "missing"); err == nil || !reflect.DeepEqual(f.calls, []string{"read"}) {
		t.Fatal("missing reread must stop deletion")
	}
	for _, protocol := range []any{nil, int32(-1), int32(3), int(1)} {
		f := &removalFixture{conv: &storedConversation{Metadata: map[string]any{"protocol": protocol}}}
		if err := (scheduledConversationRemoval{Read: f, State: f}).DeleteLocalConversationUnchecked(context.Background(), "invalid"); err == nil || len(f.calls) != 1 {
			t.Fatal("invalid protocol reached effects")
		}
	}
}

func TestRemovalGroupIdentifierNativeVersions(t *testing.T) {
	makeGroup := func(version uint16, sub, domain string) []byte {
		b := make([]byte, 21)
		binary.BigEndian.PutUint16(b, version)
		b[19] = 1
		b[20] = byte(len(sub))
		b = append(b, sub...)
		if version != 1 || sub != "" {
			b = append(b, 0, 0, 0, 7)
		}
		if version != 1 {
			b = append(b, byte(len(domain)>>8), byte(len(domain)))
		}
		return append(b, domain...)
	}
	for _, version := range []uint16{1, 2, 0, 65535} {
		for _, sub := range []string{"", "sub"} {
			for _, domain := range []string{"", "synthetic.example", "domain with space", "é"} {
				b := makeGroup(version, sub, domain)
				if version != 1 {
					b = append(b, 255, 0, 255)
				}
				id, err := removalGroupConversation(b)
				if err != nil || id != "00000000-0000-0000-0000-000000000001" {
					t.Fatalf("v%d/%q/%q: %s %v", version, sub, domain, id, err)
				}
			}
		}
	}
	valid := makeGroup(2, "sub", "synthetic.example")
	for n := 0; n < len(valid); n++ {
		if _, err := removalGroupConversation(valid[:n]); err == nil {
			t.Fatalf("accepted truncated group length %d", n)
		}
	}
	badType := append([]byte(nil), valid...)
	binary.BigEndian.PutUint16(badType[2:4], 4)
	badLength := append([]byte(nil), valid...)
	badLength[20] = 255
	badDomain := makeGroup(2, "", string([]byte{255}))
	for _, bad := range [][]byte{badType, badLength, badDomain, makeGroup(1, "bad sub", "example"), makeGroup(1, "é", "example")} {
		if _, err := removalGroupConversation(bad); err == nil {
			t.Fatal("accepted malformed stored group")
		}
	}
	// Migration must reject bad group IDs before touching either backing store.
	if err := (scheduledRemovalStore{Location: storageMigrating}).RemoveMLSClients(context.Background(), nil); err == nil || err.Error() != "invalid stored MLS group identifier" {
		t.Fatalf("bad group reached storage: %v", err)
	}
}
