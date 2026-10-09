// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"encoding/binary"
	"errors"
	"sort"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"
)

type removalSubConversation struct {
	ID    string
	Group []byte
}
type conversationRemovalState interface {
	SubConversations(context.Context, string) ([]removalSubConversation, error)
	DeleteSubConversation(context.Context, string, string) error
	RemoveMLSClients(context.Context, []byte) error
	RemoveHistoryClients(context.Context, []byte) error
	DeleteProposals(context.Context, []byte) error
	DeleteConversationCode(context.Context, string) error
	DeleteConversationState(context.Context, string, *string) error
}
type scheduledConversationRemoval struct {
	Read  conversationStore
	State conversationRemovalState
}

func (s scheduledConversationRemoval) Conversation(ctx context.Context, id string) (*storedConversation, error) {
	if s.Read == nil {
		return nil, errors.New("conversation removal reader unavailable")
	}
	return s.Read.Conversation(ctx, id)
}
func (s scheduledConversationRemoval) DeleteLocalConversationUnchecked(ctx context.Context, id string) error {
	if s.State == nil {
		return errors.New("conversation removal state unavailable")
	}
	conv, err := s.Conversation(ctx, id)
	if err != nil {
		return err
	}
	if conv == nil {
		return errors.New("conversation not found during removal")
	}
	// Every supported protocol allows ConversationDelete. No authorization or
	// notifications occur on this source unchecked path.
	group, ok := conv.Metadata["group_id"].([]byte)
	protocol, typed := conv.Metadata["protocol"].(int32)
	if !typed || protocol < 0 || protocol > 2 {
		return errors.New("invalid removal conversation protocol")
	}
	if protocol != 0 && ok && group != nil {
		subs, err := s.State.SubConversations(ctx, conv.ID)
		if err != nil {
			return err
		}
		// Source Map.assocs orders ids; adapters collapse duplicates like Map.fromList.
		sort.Slice(subs, func(i, j int) bool { return subs[i].ID < subs[j].ID })
		for _, sub := range subs {
			if err := s.State.DeleteSubConversation(ctx, conv.ID, sub.ID); err != nil {
				return err
			}
			if err := s.deleteGroup(ctx, sub.Group); err != nil {
				return err
			}
		}
		if err := s.deleteGroup(ctx, group); err != nil {
			return err
		}
	}
	if err := s.State.DeleteConversationCode(ctx, conv.ID); err != nil {
		return err
	}
	return s.State.DeleteConversationState(ctx, conv.ID, conv.Team)
}
func (s scheduledConversationRemoval) deleteGroup(ctx context.Context, group []byte) error {
	if err := s.State.RemoveMLSClients(ctx, group); err != nil {
		return err
	}
	if err := s.State.RemoveHistoryClients(ctx, group); err != nil {
		return err
	}
	return s.State.DeleteProposals(ctx, group)
}

// Migration uses the UUID parsed from the GroupId, not the caller's conversation
// id. Native unknown versions use the extensible v2 format, allowing trailing
// extensions. Domain is raw UTF8 here, not a newly validated DNS input.
func removalGroupConversation(group []byte) (string, error) {
	invalid := errors.New("invalid stored MLS group identifier")
	if len(group) < 21 {
		return "", invalid
	}
	version := binary.BigEndian.Uint16(group[:2])
	if binary.BigEndian.Uint16(group[2:4]) > 3 {
		return "", invalid
	}
	id := pgtype.UUID{Valid: true}
	copy(id.Bytes[:], group[4:20])
	n := int(group[20])
	offset := 21 + n
	if offset > len(group) {
		return "", invalid
	}
	for _, c := range group[21:offset] {
		if c < 33 || c > 126 {
			return "", invalid
		}
	}
	if version == 1 && n == 0 {
		if !utf8.Valid(group[offset:]) {
			return "", invalid
		}
	} else {
		if offset+4 > len(group) {
			return "", invalid
		}
		offset += 4
		if version == 1 {
			if !utf8.Valid(group[offset:]) {
				return "", invalid
			}
		} else {
			if offset+2 > len(group) {
				return "", invalid
			}
			length := int(binary.BigEndian.Uint16(group[offset : offset+2]))
			offset += 2
			if offset+length > len(group) || !utf8.Valid(group[offset:offset+length]) {
				return "", invalid
			}
		}
	}
	return postgresUUIDString(id), nil
}
