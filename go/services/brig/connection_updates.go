// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"github.com/apache/cassandra-gocql-driver/v2"
)

var errConnectionNotConnected = errors.New("users are not connected")

type connectionUpdateService struct {
	local       connectionLocalService
	mls         bool
	accept      func(context.Context, gocql.UUID, gocql.UUID, *string, string) (int, error)
	block       func(context.Context, gocql.UUID, gocql.UUID, string, string) error
	unblock     func(context.Context, gocql.UUID, gocql.UUID, string, *string, string) (int, error)
	established func(context.Context, gocql.UUID, gocql.UUID, string, string) (int, error)
}

// The namespace is binary; user IDs and domains are their native textual
// representations. Native orders domain then UUID before hashing.
func connectionOne2One(self, target gocql.UUID, ownDomain, otherDomain string, mls bool) (gocql.UUID, string) {
	if ownDomain > otherDomain || ownDomain == otherDomain && bytes.Compare(self[:], target[:]) > 0 {
		self, target = target, self
		ownDomain, otherDomain = otherDomain, ownDomain
	}
	ns := "9a51edb8-060c-0d9a-0c29-50a85d152982"
	if mls {
		ns = "95589dd5-b045-40dc-a6aa-dd9c4fad1c2f"
	}
	namespace, _ := gocql.ParseUUID(ns)
	data := append([]byte{}, namespace[:]...)
	data = append(data, []byte(self.String()+ownDomain+target.String()+otherDomain)...)
	digest := sha256.Sum256(data)
	var id gocql.UUID
	copy(id[:], digest[:16])
	id[6] = (id[6] & 0x0f) | 0x50
	id[8] = (id[8] & 0x3f) | 0x80
	domain := ownDomain
	if digest[16]&0x80 != 0 {
		domain = otherDomain
	}
	return id, domain
}
func (s connectionUpdateService) apply(ctx context.Context, self, target gocql.UUID, want int32, connection *string, requestID string) (*storedConnection, error) {
	l := s.local
	forward, err := l.get(ctx, self, target, l.domain)
	if err != nil {
		return nil, err
	}
	if forward == nil {
		return nil, errConnectionNotConnected
	}
	reverse, err := l.get(ctx, target, self, l.domain)
	if err != nil {
		return nil, err
	}
	if reverse == nil {
		return nil, errConnectionNotConnected
	}
	if forward.relation >= 6 || reverse.relation >= 6 || want >= 6 {
		return nil, errConnectionTransition
	}
	change := func(row storedConnection, relation int32) (*storedConnection, error) {
		r, err := l.change(ctx, row, relation)
		return &r, err
	}
	otherEvent := func(row storedConnection, named bool) error {
		var name *string
		if named {
			user, err := l.user(ctx, self)
			if err != nil {
				return err
			}
			if user != nil {
				name = &user.Name
			}
		}
		return l.event(ctx, self, connection, requestID, row, name)
	}
	accept := func() (*storedConnection, error) {
		if err := l.checkLimit(ctx, self); err != nil {
			return nil, err
		}
		convType := -1
		if forward.conversation != nil {
			convType, err = s.accept(ctx, self, *forward.conversation, connection, requestID)
			if err != nil {
				return nil, err
			}
		}
		if reverse.relation == 4 || reverse.relation == 2 {
			r := int32(0)
			if convType == 3 {
				r = 1
			}
			row, err := change(*reverse, r)
			if err != nil {
				return nil, err
			}
			if err = otherEvent(*row, true); err != nil {
				return nil, err
			}
		}
		return change(*forward, 0)
	}
	block := func() (*storedConnection, error) {
		if forward.conversation != nil {
			if err := s.block(ctx, self, *forward.conversation, forward.conversationDomain, requestID); err != nil {
				return nil, err
			}
		}
		if s.mls {
			state, err := s.established(ctx, self, target, l.domain, requestID)
			if err != nil {
				return nil, err
			}
			if state == 1 {
				id, domain := connectionOne2One(self, target, l.domain, l.domain, true)
				if err = s.block(ctx, self, id, domain, requestID); err != nil {
					return nil, err
				}
			}
		}
		return change(*forward, 1)
	}
	unblock := func() (*storedConnection, error) {
		relation := int32(4)
		if reverse.relation == 0 || reverse.relation == 1 || reverse.relation == 4 {
			relation = 0
		}
		if err := l.checkLimit(ctx, self); err != nil {
			return nil, err
		}
		convType := -1
		if forward.conversation != nil {
			convType, err = s.unblock(ctx, self, *forward.conversation, forward.conversationDomain, connection, requestID)
			if err != nil {
				return nil, err
			}
		}
		if s.mls {
			state, err := s.established(ctx, self, target, l.domain, requestID)
			if err != nil {
				return nil, err
			}
			if state == 1 || state == 2 {
				id, domain := connectionOne2One(self, target, l.domain, l.domain, true)
				if _, err = s.unblock(ctx, self, id, domain, connection, requestID); err != nil {
					return nil, err
				}
			}
		}
		if reverse.relation == 4 && relation == 0 {
			r := int32(0)
			if convType == 3 {
				r = 1
			}
			row, err := change(*reverse, r)
			if err != nil {
				return nil, err
			}
			if err = otherEvent(*row, true); err != nil {
				return nil, err
			}
		}
		return change(*forward, relation)
	}
	var result *storedConnection
	switch {
	case forward.relation == 2 && want == 1, forward.relation == 3 && want == 1, forward.relation == 0 && want == 1, forward.relation == 4 && want == 1, forward.relation == 5 && want == 1:
		result, err = block()
	case forward.relation == 2 && want == 3:
		result, err = change(*forward, 3)
	case (forward.relation == 2 || forward.relation == 3) && want == 0:
		result, err = accept()
	case forward.relation == 1 && (want == 0 || want == 4):
		result, err = unblock()
	case forward.relation == 4 && reverse.relation == 4 && want == 0:
		if _, err = change(*forward, 0); err == nil {
			result, err = change(*reverse, 0)
		}
	case forward.relation == 4 && reverse.relation == 0 && want == 0:
		result, err = change(*forward, 0)
	case forward.relation == 4 && (reverse.relation == 1 || reverse.relation == 5) && want == 5:
		result, err = change(*forward, 5)
	case forward.relation == 4 && (reverse.relation == 2 || reverse.relation == 3) && want == 5:
		if forward.conversation != nil {
			err = s.block(ctx, self, *forward.conversation, forward.conversationDomain, requestID)
		}
		if err == nil {
			var row *storedConnection
			row, err = change(*reverse, 5)
			if err == nil {
				err = otherEvent(*row, false)
			}
		}
		if err == nil {
			result, err = change(*forward, 5)
		}
	case forward.relation == want:
		return nil, nil
	default:
		return nil, errConnectionTransition
	}
	if err != nil {
		return nil, err
	}
	if result != nil {
		err = l.event(ctx, self, connection, requestID, *result, nil)
	}
	return result, err
}
