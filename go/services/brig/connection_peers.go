// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/apache/cassandra-gocql-driver/v2"
	"io"
	"net/http"
	"time"
)

func (p brigPeers) connectionTeam(ctx context.Context, user gocql.UUID, requestID string) (*gocql.UUID, error) {
	raw, missing, err := p.galleyRPC(ctx, http.MethodGet, "/i/users/"+user.String()+"/team", requestID, nil, true, nil)
	if err != nil || missing {
		return nil, err
	}
	var text string
	if json.Unmarshal(raw, &text) != nil {
		return nil, errors.New("invalid binding team response")
	}
	id, err := gocql.ParseUUID(text)
	return &id, err
}
func (p brigPeers) connectionLegalhold(ctx context.Context, user *storedUser, requestID string) (string, error) {
	if user.Team == nil {
		return "no_consent", nil
	}
	raw, missing, err := p.galleyRPC(ctx, http.MethodGet, "/i/teams/"+user.Team.String()+"/members/"+user.ID.String(), requestID, nil, true, &user.ID)
	if err != nil {
		return "", err
	}
	if missing {
		return "no_consent", nil
	}
	var member struct {
		Status *string `json:"legalhold_status"`
	}
	if json.Unmarshal(raw, &member) != nil {
		return "", errors.New("invalid legalhold member response")
	}
	if member.Status == nil {
		return "no_consent", nil
	}
	switch *member.Status {
	case "no_consent", "disabled", "pending", "enabled":
		return *member.Status, nil
	default:
		return "", errors.New("invalid legalhold member status")
	}
}

// Connection mutations must preserve Z-Connection and the native Location
// header. Existing activation RPC projections intentionally discard both.
func (p brigPeers) connectionGalley(ctx context.Context, method, path string, user gocql.UUID, connection, requestID string, body []byte) ([]byte, string, error) {
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, "http://"+p.galley.Address()+path, bytes.NewReader(body))
		if err != nil {
			return nil, "", err
		}
		req.Header.Set("Z-User", user.String())
		req.Header.Set("Request-Id", requestID)
		if connection != "" {
			req.Header.Set("Z-Connection", connection)
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		res, err := p.client.Do(req)
		retry := err != nil && retryableBrigRPC(err)
		if err == nil {
			raw, readErr := io.ReadAll(io.LimitReader(res.Body, 4<<20+1))
			res.Body.Close()
			if res.StatusCode >= 200 && res.StatusCode < 300 && readErr == nil && len(raw) <= 4<<20 {
				return raw, res.Header.Get("Location"), nil
			}
			retry = res.StatusCode >= 500 && res.StatusCode <= 599 || readErr != nil && retryableBrigRPC(readErr)
		}
		if !retry || attempt >= 3 || ctx.Err() != nil {
			return nil, "", errors.New("connection conversation request failed")
		}
		if waitBrigRetry(ctx, 100*time.Millisecond<<attempt) != nil {
			return nil, "", errors.New("connection conversation request canceled")
		}
	}
}
func (p brigPeers) createConnectionConversation(ctx context.Context, self, target gocql.UUID, connection, requestID, domain string) (gocql.UUID, error) {
	body, err := json.Marshal(map[string]any{"qualified_recipient": map[string]string{"id": target.String(), "domain": domain}, "recipient": target.String()})
	if err != nil {
		return gocql.UUID{}, err
	}
	_, location, err := p.connectionGalley(ctx, http.MethodPost, "/i/conversations/connect", self, connection, requestID, body)
	if err != nil {
		return gocql.UUID{}, err
	}
	return gocql.ParseUUID(location)
}
func (p brigPeers) acceptConnectionConversation(ctx context.Context, self, conv gocql.UUID, connection, requestID string) (int, error) {
	raw, _, err := p.connectionGalley(ctx, http.MethodPut, "/i/conversations/"+conv.String()+"/accept/v2", self, connection, requestID, nil)
	if err != nil {
		return 0, err
	}
	var value struct {
		Type *int `json:"type"`
	}
	if json.Unmarshal(raw, &value) != nil || value.Type == nil {
		return 0, errors.New("invalid accepted conversation")
	}
	return *value.Type, nil
}
