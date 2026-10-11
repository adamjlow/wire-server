// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

var errConnectionInvalidUser = errors.New("invalid connection user")
var errConnectionNoIdentity = errors.New("connection self not activated")
var errConnectionSameTeam = errors.New("connection same binding team")
var errConnectionTransition = errors.New("invalid connection transition")
var errConnectionLimit = errors.New("connection limit reached")

type connectionLocalService struct {
	domain             string
	limit              int64
	activated          func(context.Context, gocql.UUID) (bool, error)
	user               func(context.Context, gocql.UUID) (*storedUser, error)
	legalhold          func(context.Context, *storedUser, string) (string, error)
	team               func(context.Context, gocql.UUID, string) (*gocql.UUID, error)
	get                func(context.Context, gocql.UUID, gocql.UUID, string) (*storedConnection, error)
	count              func(context.Context, gocql.UUID) (int64, error)
	insert             func(context.Context, storedConnection) error
	update             func(context.Context, storedConnection) error
	createConversation func(context.Context, gocql.UUID, gocql.UUID, string, string) (gocql.UUID, error)
	acceptConversation func(context.Context, gocql.UUID, gocql.UUID, string, string) (int, error)
	notify             func(context.Context, []byte, string)
	now                func() time.Time
}

func (s connectionStore) count(ctx context.Context, user gocql.UUID) (int64, error) {
	var count int64
	for _, query := range []string{`SELECT status FROM connection WHERE left = ?`, `SELECT status FROM connection_remote WHERE left = ?`} {
		iter := s.cassandra.Query(query, user).WithContext(ctx).Consistency(gocql.One).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).Iter()
		var status int32
		for iter.Scan(&status) {
			if _, err := connectionRelation(status); err != nil {
				_ = iter.Close()
				return 0, err
			}
			if status == 0 || status == 4 {
				count++
			}
		}
		if err := iter.Close(); err != nil {
			return 0, err
		}
	}
	return count, nil
}
func (s connectionLocalService) checkLimit(ctx context.Context, user gocql.UUID) error {
	count, err := s.count(ctx, user)
	if err != nil {
		return err
	}
	if count >= s.limit {
		return errConnectionLimit
	}
	return nil
}
func (s connectionLocalService) event(ctx context.Context, origin gocql.UUID, connection *string, requestID string, row storedConnection, name *string) error {
	wire, err := row.wire()
	if err != nil {
		return err
	}
	event := map[string]any{"type": "user.connection", "connection": wire}
	if name != nil {
		event["user"] = map[string]string{"name": *name}
	}
	push := map[string]any{"recipients": []any{map[string]any{"user_id": row.from.String(), "route": "any", "clients": []string{}}}, "origin": origin.String(), "payload": []any{event}, "is_cells_event": false}
	if connection != nil {
		push["origin_connection"] = *connection
	}
	raw, err := json.Marshal([]any{push})
	if err != nil {
		return err
	}
	s.notify(ctx, raw, requestID)
	return nil
}
func (s connectionLocalService) change(ctx context.Context, row storedConnection, relation int32) (storedConnection, error) {
	row.relation = relation
	row.updated = s.now().UTC().Truncate(time.Millisecond)
	return row, s.update(ctx, row)
}
func (s connectionLocalService) create(ctx context.Context, self, target gocql.UUID, connection, requestID string) (*storedConnection, bool, error) {
	if self == target {
		return nil, false, errConnectionInvalidUser
	}
	active, err := s.activated(ctx, self)
	if err != nil {
		return nil, false, err
	}
	if !active {
		return nil, false, errConnectionNoIdentity
	}
	active, err = s.activated(ctx, target)
	if err != nil {
		return nil, false, err
	}
	if !active {
		return nil, false, errConnectionInvalidUser
	}
	asker, err := s.user(ctx, self)
	if err != nil {
		return nil, false, err
	}
	other, err := s.user(ctx, target)
	if err != nil {
		return nil, false, err
	}
	if asker == nil || other == nil {
		return nil, false, errConnectionInvalidUser
	}
	ownLH, err := s.legalhold(ctx, asker, requestID)
	if err != nil {
		return nil, false, err
	}
	otherLH, err := s.legalhold(ctx, other, requestID)
	if err != nil {
		return nil, false, err
	}
	if ownLH == "no_consent" && otherLH == "enabled" || otherLH == "no_consent" && ownLH == "enabled" {
		return nil, false, errPrekeyConsent
	}
	ownTeam, err := s.team(ctx, self, requestID)
	if err != nil {
		return nil, false, err
	}
	otherTeam, err := s.team(ctx, target, requestID)
	if err != nil {
		return nil, false, err
	}
	if ownTeam != nil && otherTeam != nil && *ownTeam == *otherTeam {
		return nil, false, errConnectionSameTeam
	}
	if asker.Type != nil && *asker.Type == 2 || other.Type != nil && *other.Type == 2 {
		return nil, false, errConnectionInvalidUser
	}
	forward, err := s.get(ctx, self, target, s.domain)
	if err != nil {
		return nil, false, err
	}
	reverse, err := s.get(ctx, target, self, s.domain)
	if err != nil {
		return nil, false, err
	}
	existed := forward != nil && reverse != nil
	if existed {
		if forward.relation >= 6 || reverse.relation >= 6 || forward.relation == 1 {
			return nil, false, errConnectionTransition
		}
		if forward.relation == 0 && (reverse.relation == 0 || reverse.relation == 1) || forward.relation == 4 && reverse.relation == 1 {
			return forward, false, nil
		}
		if reverse.relation == 1 {
			row, err := s.change(ctx, *forward, 4)
			return &row, false, err
		}
		if reverse.relation == 4 || reverse.relation == 0 {
			if forward.relation != 4 && forward.relation != 0 {
				if err := s.checkLimit(ctx, self); err != nil {
					return nil, false, err
				}
			}
			convType := -1
			if forward.conversation != nil {
				convType, err = s.acceptConversation(ctx, self, *forward.conversation, connection, requestID)
				if err != nil {
					return nil, false, err
				}
			}
			f, err := s.change(ctx, *forward, 0)
			if err != nil {
				return nil, false, err
			}
			otherRelation := int32(0)
			if convType == 3 {
				otherRelation = 1
			}
			r, err := s.change(ctx, *reverse, otherRelation)
			if err != nil {
				return nil, false, err
			}
			if err = s.event(ctx, self, &connection, requestID, r, &asker.Name); err != nil {
				return nil, false, err
			}
			if err = s.event(ctx, self, &connection, requestID, f, nil); err != nil {
				return nil, false, err
			}
			return &f, false, nil
		}
	}
	if !existed || forward.relation != 4 && forward.relation != 0 {
		if err := s.checkLimit(ctx, self); err != nil {
			return nil, false, err
		}
	}
	conv, err := s.createConversation(ctx, self, target, connection, requestID)
	if err != nil {
		return nil, false, err
	}
	f := storedConnection{from: self, to: target, domain: s.domain, relation: 4, updated: s.now().UTC().Truncate(time.Millisecond), conversation: &conv, conversationDomain: s.domain}
	if err := s.insert(ctx, f); err != nil {
		return nil, false, err
	}
	r := storedConnection{from: target, to: self, domain: s.domain, relation: 2, updated: s.now().UTC().Truncate(time.Millisecond), conversation: &conv, conversationDomain: s.domain}
	if err := s.insert(ctx, r); err != nil {
		return nil, false, err
	}
	if err = s.event(ctx, self, &connection, requestID, r, &asker.Name); err != nil {
		return nil, false, err
	}
	if err = s.event(ctx, self, &connection, requestID, f, nil); err != nil {
		return nil, false, err
	}
	return &f, !existed, nil
}
