// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type activationEvents struct {
	notifications         *sessionNotifications
	journal               *userJournal
	domain, defaultLocale string
}

func activationEventBody(result *activatedIdentity, domain, defaultLocale string) ([]byte, error) {
	if result == nil {
		return nil, errors.New("activation event unavailable")
	}
	if result.first {
		if result.account == nil {
			return nil, errors.New("activation account unavailable")
		}
		profile, err := result.account.selfProfile(domain, defaultLocale)
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{"type": "user.activate", "user": profile})
	}
	return json.Marshal(map[string]any{"type": "user.update", "user": map[string]string{"id": result.user.String(), "email": result.email}})
}
func nativeSelfPush(user gocql.UUID, route string, event json.RawMessage) ([]byte, error) {
	return json.Marshal([]any{map[string]any{
		"recipients": []any{map[string]any{"user_id": user.String(), "route": route, "clients": []string{}}},
		"origin":     user.String(), "payload": []json.RawMessage{event}, "is_cells_event": false,
	}})
}
func (s activationEvents) generate(ctx context.Context, result *activatedIdentity, requestID string) error {
	if s.notifications == nil {
		return errors.New("activation notifications unavailable")
	}
	event, err := activationEventBody(result, s.domain, s.defaultLocale)
	if err != nil {
		return err
	}
	route := "direct"
	if result.first {
		route = "any"
	}
	push, err := nativeSelfPush(result.user, route, event)
	if err != nil {
		return errors.New("activation notification encoding failed")
	}
	s.notifications.submitPush(ctx, push, requestID)
	if s.journal == nil {
		return nil
	}
	at := s.journal.now()
	if result.first {
		profile, err := result.account.selfProfile(s.domain, s.defaultLocale)
		if err != nil {
			return err
		}
		return s.journal.enqueue(ctx, encodeUserJournal(0, result.user, at, profile.Email, &profile.Locale, &profile.Name, result.account.Team))
	}
	return s.journal.enqueue(ctx, encodeUserJournal(2, result.user, at, &result.email, nil, nil, nil))
}
