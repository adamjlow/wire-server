// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func (p brigPeers) guardPrekeyLegalhold(ctx context.Context, protectee gocql.UUID, clients map[string][]string, requestID string) error {
	normalized := make(map[string][]string, len(clients))
	for user, ids := range clients {
		sorted, err := nativeClientIDSet(ids)
		if err != nil {
			return err
		}
		normalized[user] = sorted
	}
	body, err := json.Marshal(map[string]any{"glhProtectee": map[string]string{"tag": "ProtectedUser", "contents": protectee.String()}, "glhUserClients": normalized})
	if err != nil {
		return err
	}
	raw, status, err := p.galleyRPCStatus(ctx, http.MethodPut, "/i/guard-legalhold-policy-conflicts", requestID, body, true, true, nil)
	if err != nil {
		return err
	}
	switch status {
	case 200, 404:
		return nil
	case 403:
		var response struct {
			Label string `json:"label"`
		}
		_ = json.Unmarshal(raw, &response)
		if response.Label == "missing-legalhold-consent-old-clients" {
			return errPrekeyOldConsent
		}
		return errPrekeyConsent
	default:
		return errPrekeyConsent
	}
}
