// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAdminlessOriginalSelectionVectors(t *testing.T) {
	// Unchanged AdminlessGroupsSpec vectors; source assertions are not modified.
	candidate := func(id, name string) adminlessCandidate {
		return adminlessCandidate{User: qualifiedUser{ID: "00000000-0000-0000-0000-00000000000" + id, Domain: "wire.com"}, Name: name}
	}
	alice, bob1, bob2, charlie := candidate("1", "alice"), candidate("2", "bob"), candidate("4", "bob"), candidate("3", "charlie")
	input := []adminlessCandidate{charlie, bob1, bob2, alice}
	for _, tc := range []struct {
		strategy string
		seed     uint64
		want     []qualifiedUser
	}{{"alphabetical", 0, []qualifiedUser{alice.User}}, {"random", 2, []qualifiedUser{bob2.User}}, {"all", 0, []qualifiedUser{alice.User, bob1.User, bob2.User, charlie.User}}} {
		got, err := selectAdminlessCandidates(tc.seed, tc.strategy, input)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%s got %v error %v", tc.strategy, got, err)
		}
	}
	if _, err := selectAdminlessCandidates(0, "unsupported", input); err == nil {
		t.Fatal("unknown strategy accepted")
	}
}

type adminlessFixture struct {
	mutex     sync.Mutex
	conv      *storedConversation
	feature   adminlessFeature
	users     []adminlessUser
	ids       []string
	calls     []string
	fail      string
	supported map[string]bool
	removalFixture
}

func (f *adminlessFixture) step(name string) error {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.calls = append(f.calls, name)
	if f.fail == name {
		return errors.New("synthetic failure")
	}
	return nil
}
func (f *adminlessFixture) Conversation(context.Context, string) (*storedConversation, error) {
	err := f.step("read")
	return f.conv, err
}
func (f *adminlessFixture) TeamConversations(context.Context, string) ([]string, error) {
	err := f.step("team")
	return f.ids, err
}
func (f *adminlessFixture) Feature(context.Context, string) (adminlessFeature, error) {
	err := f.step("feature")
	return f.feature, err
}
func (f *adminlessFixture) Users(_ context.Context, ids []string) ([]adminlessUser, error) {
	err := f.step("users:" + strings.Join(ids, ","))
	return f.users, err
}
func (f *adminlessFixture) Promote(_ context.Context, _ *storedConversation, u qualifiedUser) error {
	return f.step("promote:" + u.ID)
}
func (f *adminlessFixture) SupportsSystemDelete(_ context.Context, domain string) (bool, error) {
	err := f.step("support:" + domain)
	return f.supported[domain], err
}
func (f *adminlessFixture) Promoted(_ context.Context, _ *storedConversation, u qualifiedUser, actor *string, _ string) error {
	return f.step("notify:" + u.ID + actorSuffix(actor))
}
func (f *adminlessFixture) Deleted(_ context.Context, _ *storedConversation, actor *string, _ string) error {
	return f.step("delete" + actorSuffix(actor))
}
func (f *adminlessFixture) Reminder(_ context.Context, _ *storedConversation, actor *string, _ string, _ time.Time) error {
	return f.step("reminder" + actorSuffix(actor))
}
func (f *adminlessFixture) Schedule(context.Context, *storedConversation, *string, string, adminlessFeature) error {
	return f.step("schedule")
}
func actorSuffix(actor *string) string {
	if actor == nil {
		return ":system"
	}
	return ":user"
}
func newAdminlessFixture() *adminlessFixture {
	team := "00000000-0000-0000-0000-000000000001"
	return &adminlessFixture{conv: &storedConversation{ID: team, Team: &team, LocalMembers: []localConversationMember{{ID: "b", Role: "wire_member"}, {ID: "a", Role: "wire_member"}}, Metadata: map[string]any{"type": int32(0), "protocol": int32(0)}}, feature: adminlessFeature{Enabled: true, Strategy: "all"}, users: []adminlessUser{{ID: "b", Name: "bob", Type: "regular", Status: "active"}, {ID: "a", Name: "alice", Type: "regular", Status: "active"}}, supported: map[string]bool{}, ids: []string{team, team}}
}
func (f *adminlessFixture) dispatcher() adminlessDispatcher {
	return adminlessDispatcher{Store: f, Notify: f, Schedule: f, Domain: "wire.com", Seed: func() (uint64, error) {
		if err := f.step("seed"); err != nil {
			return 0, err
		}
		return 0, nil
	}, Removal: scheduledConversationRemoval{State: &f.removalFixture}}
}
func TestAdminlessPromotionAndSourceFailureBoundaries(t *testing.T) {
	order := []string{"read", "feature", "users:a,b", "seed", "promote:a", "notify:a:system", "promote:b", "notify:b:system"}
	for failure := -1; failure < len(order); failure++ {
		f := newAdminlessFixture()
		want := order
		if failure >= 0 {
			f.fail = order[failure]
			want = order[:failure+1]
		}
		err := f.dispatcher().Dispatch(t.Context(), scheduledJobPayload{Type: "adminless_deletion", Conversation: f.conv.ID})
		if (err != nil) != (failure >= 0) || !reflect.DeepEqual(f.calls, want) {
			t.Fatalf("failure%d got%v err%v", failure, f.calls, err)
		}
		if len(f.removalFixture.calls) != 0 {
			t.Fatal("promotion removed conversation")
		}
	}
	// Account eligibility comes from Brig status/type/service, not merely membership.
	for _, field := range []string{"type", "status", "service", "missing"} {
		f := newAdminlessFixture()
		f.conv.LocalMembers = f.conv.LocalMembers[:1]
		f.users = f.users[:1]
		switch field {
		case "type":
			f.users[0].Type = "app"
		case "status":
			f.users[0].Status = "suspended"
		case "service":
			f.users[0].Service = true
		case "missing":
			f.users = nil
		}
		if err := f.dispatcher().Dispatch(t.Context(), scheduledJobPayload{Type: "adminless_setup", Team: *f.conv.Team}); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(strings.Join(f.calls, ","), "seed") || !strings.Contains(strings.Join(f.calls, ","), "schedule") {
			t.Fatalf("ineligible %s promoted: %v", field, f.calls)
		}
	}
}
func TestAdminlessGuardsCapabilitiesAndNotifications(t *testing.T) {
	for _, guard := range []string{"nonregular", "meeting", "unteamed", "disabled", "localadmin", "remoteadmin"} {
		f := newAdminlessFixture()
		switch guard {
		case "nonregular":
			f.conv.Metadata["type"] = int32(1)
		case "meeting":
			kind := int32(2)
			f.conv.Metadata["group_conv_type"] = &kind
		case "unteamed":
			f.conv.Team = nil
		case "disabled":
			f.feature.Enabled = false
		case "localadmin":
			f.conv.LocalMembers[0].Role = "wire_admin"
		case "remoteadmin":
			f.conv.RemoteMembers = []remoteConversationMember{{Role: "wire_admin"}}
		}
		if err := f.dispatcher().Dispatch(t.Context(), scheduledJobPayload{Type: "adminless_deletion", Conversation: f.conv.ID}); err != nil {
			t.Fatal(err)
		}
		if len(f.calls) > 2 || len(f.removalFixture.calls) > 0 {
			t.Fatalf("guard%s reached effects %v", guard, f.calls)
		}
	}
	f := newAdminlessFixture()
	f.users = nil
	f.conv.RemoteMembers = []remoteConversationMember{{User: qualifiedUser{Domain: "first.example"}}, {User: qualifiedUser{Domain: "second.example"}}, {User: qualifiedUser{Domain: "first.example"}}}
	// One cache per setup job, including negative responses; evaluate both remotes.
	if err := f.dispatcher().Dispatch(t.Context(), scheduledJobPayload{Type: "adminless_setup", Team: *f.conv.Team}); err != nil {
		t.Fatal(err)
	}
	want := []string{"team", "read", "feature", "users:a,b", "support:first.example", "support:second.example", "read", "feature", "users:a,b"}
	if !reflect.DeepEqual(f.calls, want) {
		t.Fatalf("capability cache %v", f.calls)
	}
	for _, system := range []bool{true, false} {
		f.calls = nil
		f.removalFixture.calls = nil
		job := scheduledJobPayload{Type: "adminless_deletion", Conversation: f.conv.ID}
		if !system {
			actor := "synthetic-user"
			job.Actor = &actor
		}
		if err := f.dispatcher().Dispatch(t.Context(), job); err != nil {
			t.Fatal(err)
		}
		if system {
			if len(f.removalFixture.calls) != 0 {
				t.Fatal("unsupported system delete occurred")
			}
		} else {
			if !reflect.DeepEqual(f.removalFixture.calls, []string{"code", "state"}) || f.calls[len(f.calls)-1] != "delete:user" {
				t.Fatalf("user deletion %v %v", f.calls, f.removalFixture.calls)
			}
			// Domain deletion uses the snapshot it already read, without an extra reread.
		}
	}
	f.calls = nil
	when := time.Now().UTC()
	job := scheduledJobPayload{Type: "adminless_reminder", Conversation: f.conv.ID, DeletionScheduledFor: &when}
	if err := f.dispatcher().Dispatch(t.Context(), job); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.calls, []string{"read", "feature", "users:a,b", "reminder:system"}) {
		t.Fatalf("reminder unexpectedly gated by deletion support: %v", f.calls)
	}
}

func TestAdminlessCapabilityFailureIsUnsupported(t *testing.T) {
	f := newAdminlessFixture()
	f.users = nil
	f.fail = "support:remote.example"
	f.supported["remote.example"] = true
	f.conv.RemoteMembers = []remoteConversationMember{{User: qualifiedUser{Domain: "remote.example"}}}
	for _, kind := range []string{"adminless_setup", "adminless_deletion"} {
		f.calls = nil
		if err := f.dispatcher().Dispatch(t.Context(), scheduledJobPayload{Type: kind, Team: *f.conv.Team, Conversation: f.conv.ID}); err != nil {
			t.Fatalf("federation capability failure must skip rather than fail job: %v", err)
		}
		for _, call := range f.calls {
			if call == "schedule" || call == "delete:system" {
				t.Fatal("failed capability permitted system delete")
			}
		}
	}
}
