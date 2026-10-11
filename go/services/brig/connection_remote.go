// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/apache/cassandra-gocql-driver/v2"
)

var errConnectionNotFederating = errors.New("connection team not federating")

type connectionRemoteService struct {
	local       connectionLocalService
	mls         bool
	call        func(context.Context, string, string, string, any) (json.RawMessage, error)
	allowed     func(context.Context, string, *gocql.UUID) (bool, error)
	upsert      func(context.Context, gocql.UUID, gocql.UUID, string, gocql.UUID, string, bool, bool, string) error
	established func(context.Context, gocql.UUID, gocql.UUID, string, string) (int, error)
}

// Actions: connect, block, ignore, rescind. Remote actions are connect/rescind.
func remoteConnectionTransition(old int32, action string, remote bool) (int32, bool) {
	if old >= 6 {
		old = 1
	}
	if remote {
		switch action {
		case "connect":
			if old == 1 {
				return 0, false
			}
			if old == 4 || old == 0 {
				return 0, true
			}
			return 2, true
		case "rescind":
			if old == 2 {
				return 5, true
			}
			if old == 0 {
				return 4, true
			}
			return 0, false
		}
	}
	switch action {
	case "connect":
		if old == 2 || old == 0 {
			return 0, true
		}
		return 4, true
	case "block":
		return 1, old != 1
	case "ignore":
		return 3, old == 2
	case "rescind":
		if old == 4 {
			return 5, true
		}
		if old == 0 {
			return 2, true
		}
	}
	return 0, false
}
func remoteConnectionIncluded(relation int32, remote bool) bool {
	if remote {
		return relation == 0 || relation == 2 || relation == 3
	}
	return relation == 0 || relation == 4
}
func (s connectionRemoteService) transition(ctx context.Context, self, target gocql.UUID, domain string, previous *storedConnection, relation int32, changed, remote bool, connection *string, requestID string) (*storedConnection, bool, error) {
	if previous == nil && !changed {
		return nil, false, errConnectionTransition
	}
	if !changed {
		return previous, false, nil
	}
	conv, convDomain := connectionOne2One(self, target, s.local.domain, domain, false)
	if previous != nil && previous.conversation != nil {
		conv = *previous.conversation
		convDomain = previous.conversationDomain
	}
	if err := s.upsert(ctx, self, target, domain, conv, convDomain, remote, remoteConnectionIncluded(relation, remote), requestID); err != nil {
		return nil, false, err
	}
	if previous != nil && s.mls {
		state, err := s.established(ctx, self, target, domain, requestID)
		if err != nil {
			return nil, false, err
		}
		if state == 1 || state == 2 && previous.relation == 1 && relation == 0 {
			mlsConv, mlsDomain := connectionOne2One(self, target, s.local.domain, domain, true)
			if err = s.upsert(ctx, self, target, domain, mlsConv, mlsDomain, remote, remoteConnectionIncluded(relation, remote), requestID); err != nil {
				return nil, false, err
			}
		}
	}
	var row storedConnection
	var err error
	if previous == nil {
		row = storedConnection{from: self, to: target, domain: domain, relation: relation, updated: s.local.now(), conversation: &conv, conversationDomain: convDomain}
		err = s.local.insert(ctx, row)
	} else {
		row, err = s.local.change(ctx, *previous, relation)
	}
	if err != nil {
		return nil, false, err
	}
	if err = s.local.event(ctx, self, connection, requestID, row, nil); err != nil {
		return nil, false, err
	}
	return &row, true, nil
}
func (s connectionRemoteService) ensureFederates(ctx context.Context, target gocql.UUID, domain, requestID string) (bool, error) {
	raw, err := s.call(ctx, domain, "get-users-by-ids", requestID, []string{target.String()})
	if err != nil {
		return false, err
	}
	var profiles []struct {
		Team *string `json:"team"`
		Type string  `json:"type"`
	}
	if json.Unmarshal(raw, &profiles) != nil {
		return false, errors.New("invalid remote connection profiles")
	}
	var team *gocql.UUID
	app := false
	if len(profiles) > 0 {
		app = profiles[0].Type == "app"
		if profiles[0].Team != nil {
			id, err := gocql.ParseUUID(*profiles[0].Team)
			if err != nil {
				return false, err
			}
			team = &id
		}
	}
	allowed, err := s.allowed(ctx, domain, team)
	if err != nil {
		return false, err
	}
	if !allowed {
		return false, errConnectionNotFederating
	}
	return app, nil
}
func (s connectionRemoteService) localAction(ctx context.Context, self, target gocql.UUID, domain, action string, connection *string, requestID string) (*storedConnection, bool, bool, error) {
	previous, err := s.local.get(ctx, self, target, domain)
	if err != nil {
		return nil, false, false, err
	}
	old := int32(5)
	if previous != nil {
		old = previous.relation
	}
	if old != 0 && old != 4 && action == "connect" {
		if err = s.local.checkLimit(ctx, self); err != nil {
			return nil, false, false, err
		}
	}
	relation, changed := remoteConnectionTransition(old, action, false)
	if changed && (action == "connect" || action == "rescind") {
		user, err := s.local.user(ctx, self)
		if err != nil {
			return nil, false, false, err
		}
		var team any
		if user != nil && user.Team != nil {
			team = user.Team.String()
		}
		remoteAction := "RemoteConnect"
		if action == "rescind" {
			remoteAction = "RemoteRescind"
		}
		raw, err := s.call(ctx, domain, "send-connection-action", requestID, map[string]any{"from": self.String(), "from_team": team, "to": target.String(), "action": remoteAction})
		if err != nil {
			return nil, false, false, err
		}
		var result struct {
			Tag      string  `json:"tag"`
			Contents *string `json:"contents"`
		}
		if json.Unmarshal(raw, &result) != nil {
			return nil, false, false, errors.New("invalid remote connection reaction")
		}
		switch result.Tag {
		case "NewConnectionResponseNotFederating":
			return nil, false, false, errConnectionNotFederating
		case "NewConnectionResponseUserNotActivated":
			return nil, false, false, errConnectionInvalidUser
		case "NewConnectionResponseOk":
			if result.Contents != nil {
				reaction := ""
				switch *result.Contents {
				case "RemoteConnect":
					reaction = "connect"
				case "RemoteRescind":
					reaction = "rescind"
				default:
					return nil, false, false, errors.New("invalid remote connection action")
				}
				if next, ok := remoteConnectionTransition(relation, reaction, true); ok {
					relation = next
				}
			}
		default:
			return nil, false, false, errors.New("invalid remote connection response")
		}
	}
	row, updated, err := s.transition(ctx, self, target, domain, previous, relation, changed, false, connection, requestID)
	return row, previous == nil, updated, err
}
func (s connectionRemoteService) create(ctx context.Context, self, target gocql.UUID, domain string, connection *string, requestID string) (*storedConnection, bool, error) {
	active, err := s.local.activated(ctx, self)
	if err != nil {
		return nil, false, err
	}
	if !active {
		return nil, false, errConnectionNoIdentity
	}
	app, err := s.ensureFederates(ctx, target, domain, requestID)
	if err != nil {
		return nil, false, err
	}
	user, err := s.local.user(ctx, self)
	if err != nil {
		return nil, false, err
	}
	if app || user != nil && user.Type != nil && *user.Type == 2 {
		return nil, false, errConnectionInvalidUser
	}
	row, created, _, err := s.localAction(ctx, self, target, domain, "connect", connection, requestID)
	return row, created, err
}
func (s connectionRemoteService) update(ctx context.Context, self, target gocql.UUID, domain string, relation int32, connection *string, requestID string) (*storedConnection, error) {
	if _, err := s.ensureFederates(ctx, target, domain, requestID); err != nil {
		return nil, err
	}
	action := ""
	switch relation {
	case 5:
		action = "rescind"
	case 4, 0:
		action = "connect"
	case 1:
		action = "block"
	case 3:
		action = "ignore"
	default:
		return nil, errConnectionTransition
	}
	row, _, updated, err := s.localAction(ctx, self, target, domain, action, connection, requestID)
	if !updated {
		return nil, err
	}
	return row, err
}
func (s connectionRemoteService) incoming(ctx context.Context, self, target gocql.UUID, domain string, team *gocql.UUID, action, requestID string) (map[string]any, error) {
	allowed, err := s.allowed(ctx, domain, team)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return map[string]any{"tag": "NewConnectionResponseNotFederating"}, nil
	}
	active, err := s.local.activated(ctx, self)
	if err != nil {
		return nil, err
	}
	if !active {
		return map[string]any{"tag": "NewConnectionResponseUserNotActivated"}, nil
	}
	previous, err := s.local.get(ctx, self, target, domain)
	if err != nil {
		return nil, err
	}
	old := int32(5)
	if previous != nil {
		old = previous.relation
	}
	relation, changed := remoteConnectionTransition(old, action, true)
	_, _, err = s.transition(ctx, self, target, domain, previous, relation, changed, true, nil, requestID)
	var reaction any
	if errors.Is(err, errConnectionTransition) {
		reaction = "RemoteRescind"
	} else if err != nil {
		return nil, err
	} else if changed && (relation == 0 || relation == 4) {
		reaction = "RemoteConnect"
	}
	return map[string]any{"tag": "NewConnectionResponseOk", "contents": reaction}, nil
}
