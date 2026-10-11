// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func (p brigPeers) deleteQueuedClient(ctx context.Context, user gocql.UUID, client string, connection *string, requestID string) error {
	id, err := parseClientID(client)
	if err != nil {
		return errors.New("invalid queued client ID")
	}
	for _, endpoint := range []Endpoint{p.galley, p.gundeck} {
		if _, _, err := p.replayableRPCStatus(ctx, endpoint, http.MethodDelete, "/i/clients/"+id, requestID, nil, false, false, &user, 200, 204, 404); err != nil {
			return err
		}
	}
	push := map[string]any{"recipients": []any{map[string]any{"user_id": user.String(), "route": "any", "clients": []string{}}}, "origin": user.String(), "payload": []any{map[string]any{"type": "user.client-remove", "client": map[string]string{"id": id}}}, "is_cells_event": false}
	if connection != nil {
		push["origin_connection"] = *connection
	}
	body, err := json.Marshal([]any{push})
	if err != nil {
		return err
	}
	return p.pushSelfEvent(ctx, body, requestID)
}
