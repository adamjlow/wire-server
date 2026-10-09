// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"errors"
)

type conversationStore interface {
	Conversation(context.Context, string) (*storedConversation, error)
	UpsertLocalMembers(context.Context, string, []string, string) error
}

type migrationConversationStore interface {
	conversationStore
	ConversationExists(context.Context, string) (bool, error)
	WithConversationLock(context.Context, string, bool, func() error) error
	CleanupConversation(context.Context, string) error
}

type routedGroupStore struct {
	Groups    groupSyncStore
	Postgres  migrationConversationStore
	Cassandra conversationStore
	Location  storageLocation
}

func (s *routedGroupStore) GroupChannels(ctx context.Context, team, group string) ([]string, bool, error) {
	return s.Groups.GroupChannels(ctx, team, group)
}

func (s *routedGroupStore) GroupMembers(ctx context.Context, team, group string) ([]string, bool, error) {
	return s.Groups.GroupMembers(ctx, team, group)
}

func (s *routedGroupStore) Conversation(ctx context.Context, id string) (conv *storedConversation, err error) {
	switch s.Location {
	case storagePostgres:
		return s.Postgres.Conversation(ctx, id)
	case storageCassandra:
		return s.Cassandra.Conversation(ctx, id)
	case storageMigrating:
		err = s.Postgres.WithConversationLock(ctx, id, false, func() error {
			if err := s.Postgres.CleanupConversation(ctx, id); err != nil {
				return err
			}
			var err error
			conv, err = s.Postgres.Conversation(ctx, id)
			if err == nil && conv == nil {
				conv, err = s.Cassandra.Conversation(ctx, id)
			}
			return err
		})
		return
	default:
		return nil, errors.New("invalid conversation storage location")
	}
}

func (s *routedGroupStore) UpsertLocalMembers(ctx context.Context, id string, users []string, role string) error {
	switch s.Location {
	case storagePostgres:
		return s.Postgres.UpsertLocalMembers(ctx, id, users, role)
	case storageCassandra:
		return s.Cassandra.UpsertLocalMembers(ctx, id, users, role)
	case storageMigrating:
		return s.Postgres.WithConversationLock(ctx, id, false, func() error {
			if err := s.Postgres.CleanupConversation(ctx, id); err != nil {
				return err
			}
			// Source writes route by EXISTS, not getConversation/toConv: an
			// incomplete MLS row can exist even though reads return Nothing.
			exists, err := s.Postgres.ConversationExists(ctx, id)
			if err != nil {
				return err
			}
			if exists {
				return s.Postgres.UpsertLocalMembers(ctx, id, users, role)
			}
			return s.Cassandra.UpsertLocalMembers(ctx, id, users, role)
		})
	default:
		return errors.New("invalid conversation storage location")
	}
}
