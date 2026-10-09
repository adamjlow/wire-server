// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type observedGroupStore struct {
	channels, users []string
	found           bool
	conv            *storedConversation
	reads           int
	added           []string
	role            string
	writeError      error
}

func (s *observedGroupStore) GroupChannels(context.Context, string, string) ([]string, bool, error) {
	return s.channels, s.found, nil
}
func (s *observedGroupStore) GroupMembers(context.Context, string, string) ([]string, bool, error) {
	return s.users, s.found, nil
}
func (s *observedGroupStore) Conversation(context.Context, string) (*storedConversation, error) {
	s.reads++
	return s.conv, nil
}
func (s *observedGroupStore) UpsertLocalMembers(_ context.Context, _ string, users []string, role string) error {
	s.added, s.role = users, role
	return s.writeError
}

type observedGroupDelivery struct {
	jobs  []backgroundJob
	joins []groupJoin
	store *observedGroupStore
	t     *testing.T
}

func (d *observedGroupDelivery) Publish(_ context.Context, job backgroundJob) error {
	d.jobs = append(d.jobs, job)
	return nil
}
func (d *observedGroupDelivery) NotifyJoin(_ context.Context, join groupJoin) error {
	if len(d.store.added) == 0 {
		d.t.Fatal("notification preceded member storage")
	}
	d.joins = append(d.joins, join)
	return nil
}

func TestGroupSyncPublishesDistinctChannelJobsWithSourceContext(t *testing.T) {
	store := &observedGroupStore{channels: []string{jobUUID, "22222222-2222-2222-2222-222222222222"}, found: true}
	delivery := &observedGroupDelivery{t: t, store: store}
	d := &groupSyncDispatcher{Store: store, Publisher: delivery, Domain: "example.com"}
	job := sourceGroupJob()
	job.Payload.Payload.Actor = ptr(jobUUID)
	if err := d.Dispatch(t.Context(), job); err != nil {
		t.Fatal(err)
	}
	if len(delivery.jobs) != 2 || delivery.jobs[0].ID == delivery.jobs[1].ID {
		t.Fatal("group sync did not create fresh jobs for each channel")
	}
	for i, child := range delivery.jobs {
		if child.RequestID != job.RequestID || child.Payload.Type != "sync-user-group-and-channel" || child.Payload.Payload.ConvID != store.channels[i] || child.Payload.Payload.Actor == nil || *child.Payload.Payload.Actor != jobUUID {
			t.Fatal("child job lost original context")
		}
		if _, err := decodeBackgroundJob(encode(t, child)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestChannelSyncAddsOnlyMissingUsersAndPreservesActorRecipientsAndBots(t *testing.T) {
	for _, actor := range []*string{nil, ptr(jobUUID)} {
		store := &observedGroupStore{found: true, users: []string{"existing", "new"}, conv: &storedConversation{ID: jobUUID, LocalMembers: []localConversationMember{{ID: "existing", Role: "wire_admin"}, {ID: "bot", Service: ptr("service"), Provider: ptr("provider")}}, RemoteMembers: []remoteConversationMember{{User: qualifiedUser{ID: "remote", Domain: "remote.example.com"}, Role: "wire_admin"}}}}
		delivery := &observedGroupDelivery{t: t, store: store}
		d := &groupSyncDispatcher{Store: store, Notifier: delivery, Domain: "example.com"}
		job := sourceGroupJob()
		job.Payload.Type = "sync-user-group-and-channel"
		job.Payload.Payload.ConvID, job.Payload.Payload.Actor = jobUUID, actor
		if err := d.Dispatch(t.Context(), job); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(store.added, []string{"new"}) || store.role != "wire_member" || len(delivery.joins) != 1 {
			t.Fatal("group sync changed existing roles or member delta")
		}
		join := delivery.joins[0]
		if !reflect.DeepEqual(join.Locals, []string{"existing", "new"}) || len(join.Remotes) != 1 || len(join.Bots) != 1 || len(join.NewUsers) != 1 || join.RequestID != job.RequestID {
			t.Fatal("group sync recipients changed")
		}
		if actor == nil && (join.Via != "scim" || join.Actor.ID != "00000000-0000-0000-0000-000000000000") || actor != nil && (join.Via != "user" || join.Actor.ID != *actor) || join.Actor.Domain != "example.com" {
			t.Fatal("source SCIM/user origin changed")
		}
	}
}

func TestChannelSyncMissingGroupMemberLimitAndWriteFailure(t *testing.T) {
	for _, mode := range []string{"missing-group", "missing-conversation", "member-limit", "write-failure", "already-member"} {
		t.Run(mode, func(t *testing.T) {
			store := &observedGroupStore{found: true, users: []string{"new"}, conv: &storedConversation{ID: jobUUID}}
			switch mode {
			case "missing-group":
				store.found = false
			case "missing-conversation":
				store.conv = nil
			case "member-limit":
				// Bots count toward the source cap even though they are excluded
				// from the local-user recipient set.
				store.conv.LocalMembers = make([]localConversationMember, 2000)
				store.conv.LocalMembers[0] = localConversationMember{ID: "bot", Service: ptr("service"), Provider: ptr("provider")}
			case "write-failure":
				store.writeError = errors.New("synthetic write failure")
			case "already-member":
				store.conv.LocalMembers = []localConversationMember{{ID: "new", Role: "wire_admin"}}
			}
			delivery := &observedGroupDelivery{t: t, store: store}
			d := &groupSyncDispatcher{Store: store, Notifier: delivery, Domain: "example.com"}
			job := sourceGroupJob()
			job.Payload.Type = "sync-user-group-and-channel"
			job.Payload.Payload.ConvID = jobUUID
			err := d.Dispatch(t.Context(), job)
			if (err != nil) != (mode == "write-failure") || len(delivery.joins) != 0 || store.reads != 1 || mode != "write-failure" && len(store.added) != 0 {
				t.Fatal("missing/limited/failed sync changed storage or notifications")
			}
		})
	}
}
