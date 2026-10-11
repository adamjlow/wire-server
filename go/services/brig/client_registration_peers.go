// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func (p brigPeers) registerGalleyClient(ctx context.Context, user gocql.UUID, id, requestID string) error {
	_, _, err := p.galleyRPC(ctx, http.MethodPost, "/i/clients/"+id, requestID, nil, false, &user)
	return err
}

func (p brigPeers) clientAddedEvent(ctx context.Context, user gocql.UUID, connection *string, client storedClient, requestID string) error {
	// Native user-event schema deliberately uses the V6 client representation,
	// independently of the version of the registration request.
	view, err := client.wire(6)
	if err != nil {
		return err
	}
	push := map[string]any{
		"recipients": []any{map[string]any{"user_id": user.String(), "route": "any", "clients": []string{}}},
		"origin":     user.String(), "payload": []any{map[string]any{"type": "user.client-add", "client": view}}, "is_cells_event": false,
	}
	if connection != nil {
		push["origin_connection"] = *connection
	}
	body, err := json.Marshal([]any{push})
	if err != nil {
		return err
	}
	// Registration events are synchronous; the best-effort property/session
	// notification worker is not the native addClient contract.
	return p.pushSelfEvent(ctx, body, requestID)
}
