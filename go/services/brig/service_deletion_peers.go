// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func (p brigPeers) deleteServiceConnection(ctx context.Context, provider, service gocql.UUID) error {
	body, err := json.Marshal(map[string]string{"id": service.String(), "provider": provider.String()})
	if err != nil {
		return err
	}
	_, _, err = p.replayableRPCStatus(ctx, p.galley, http.MethodDelete, "/i/services", "", body, false, false, nil)
	return err
}

func (p brigPeers) removeServiceBotMember(ctx context.Context, bot deletionServiceBot) error {
	body, err := json.Marshal(map[string]string{"conversation": bot.conversation.String(), "bot": bot.user.String()})
	if err != nil {
		return err
	}
	raw, status, err := p.replayableRPCStatus(ctx, p.galley, http.MethodDelete, "/i/bots", "", body, false, false, &bot.user, 200, 404)
	if err != nil || status == 404 {
		return err
	}
	if !validDeletionConversationEvent(raw) {
		return errors.New("invalid Galley bot removal event")
	}
	return nil
}
