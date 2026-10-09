// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"errors"
)

// These are storage and delivery boundaries, not alternate Galley APIs. The
// source runner reads/upserts its own conversation store and emits the ordinary
// local, remote and bot join notifications.
type groupSyncStore interface {
	GroupChannels(context.Context, string, string) ([]string, bool, error)
	GroupMembers(context.Context, string, string) ([]string, bool, error)
	Conversation(context.Context, string) (*storedConversation, error)
	UpsertLocalMembers(context.Context, string, []string, string) error
}

type qualifiedUser struct {
	ID     string `json:"id"`
	Domain string `json:"domain"`
}

type localConversationMember struct {
	ID       string
	Role     string
	Service  *string
	Provider *string
}

func (m localConversationMember) bot() bool { return m.Service != nil && m.Provider != nil }

type remoteConversationMember struct {
	User qualifiedUser
	Role string
}

type storedConversation struct {
	ID            string
	Team          *string
	LocalMembers  []localConversationMember
	RemoteMembers []remoteConversationMember
	// The storage adapter retains source metadata needed by scheduled handlers
	// and Cells routing. It must not substitute a partial API projection.
	Metadata map[string]any
}

type groupJoin struct {
	Conversation *storedConversation
	RequestID    string
	Actor        qualifiedUser
	Via          string
	NewUsers     []qualifiedUser
	Locals       []string
	Remotes      []qualifiedUser
	Bots         []localConversationMember
}

type groupJoinNotifier interface {
	NotifyJoin(context.Context, groupJoin) error
}

type groupJobPublisher interface {
	Publish(context.Context, backgroundJob) error
}

type groupSyncDispatcher struct {
	Store     groupSyncStore
	Notifier  groupJoinNotifier
	Publisher groupJobPublisher
	Domain    string
}

func (d *groupSyncDispatcher) Dispatch(ctx context.Context, job backgroundJob) error {
	if d.Store == nil || !validDomain(d.Domain) {
		return errors.New("background job storage is unavailable")
	}
	p := job.Payload.Payload
	switch job.Payload.Type {
	case "sync-user-group":
		if d.Publisher == nil {
			return errors.New("background job publisher is unavailable")
		}
		channels, _, err := d.Store.GroupChannels(ctx, p.TeamID, p.UserGroupID)
		if err != nil {
			return err
		}
		for _, convID := range channels {
			id, err := newJobID()
			if err != nil {
				return err
			}
			child := backgroundJob{ID: id, RequestID: job.RequestID, Payload: backgroundJobPayload{Type: "sync-user-group-and-channel", Payload: groupSyncPayload{TeamID: p.TeamID, UserGroupID: p.UserGroupID, ConvID: convID, Actor: p.Actor}}}
			if err := d.Publisher.Publish(ctx, child); err != nil {
				return err
			}
		}
		return nil
	case "sync-user-group-and-channel":
		return d.syncChannel(ctx, job)
	default:
		return errors.New("unknown background job type")
	}
}

func (d *groupSyncDispatcher) syncChannel(ctx context.Context, job backgroundJob) error {
	p := job.Payload.Payload
	users, found, err := d.Store.GroupMembers(ctx, p.TeamID, p.UserGroupID)
	if err != nil {
		return err
	}
	// Source still reads the conversation when the group has disappeared.
	conv, err := d.Store.Conversation(ctx, p.ConvID)
	if err != nil {
		return err
	}
	if conv == nil || !found {
		return nil
	}
	currentUsers := make(map[string]bool)
	join := groupJoin{Conversation: conv, RequestID: job.RequestID, Actor: qualifiedUser{ID: "00000000-0000-0000-0000-000000000000", Domain: d.Domain}, Via: "scim"}
	if p.Actor != nil {
		join.Actor.ID = *p.Actor
		join.Via = "user"
	}
	for _, m := range conv.LocalMembers {
		if m.bot() {
			join.Bots = append(join.Bots, m)
		} else {
			currentUsers[m.ID] = true
			join.Locals = append(join.Locals, m.ID)
		}
	}
	newUsers := []string{}
	for _, user := range users {
		if !currentUsers[user] {
			newUsers = append(newUsers, user)
			join.NewUsers = append(join.NewUsers, qualifiedUser{ID: user, Domain: d.Domain})
		}
	}
	// Match the source hardTruncationLimit, including bots in the original
	// local member count. No removals or role changes are performed by sync.
	if len(newUsers) == 0 || len(conv.LocalMembers)+len(newUsers) > 2000 {
		return nil
	}
	if d.Notifier == nil {
		return errors.New("conversation join notifier is unavailable")
	}
	if err := d.Store.UpsertLocalMembers(ctx, p.ConvID, newUsers, "wire_member"); err != nil {
		return err
	}
	seen := make(map[string]bool)
	locals := []string{}
	for _, id := range append(join.Locals, newUsers...) {
		if !seen[id] {
			locals = append(locals, id)
			seen[id] = true
		}
	}
	join.Locals = locals
	seenRemote := make(map[qualifiedUser]bool)
	for _, member := range conv.RemoteMembers {
		if !seenRemote[member.User] {
			join.Remotes = append(join.Remotes, member.User)
			seenRemote[member.User] = true
		}
	}
	// Source commits members before notification. If notification fails and
	// retry sees those members, it may skip notification; document this inherited
	// durability concern rather than invent a new transaction/outbox contract.
	return d.Notifier.NotifyJoin(ctx, join)
}
