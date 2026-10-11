// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type accountDeletionPeers struct {
	peers     brigPeers
	cargohold Endpoint
}

// Native owner lookup deliberately treats every status except 403 as an owner.
// A missing team or server error must not accidentally permit self deletion.
func (p accountDeletionPeers) owner(ctx context.Context, team, user gocql.UUID, requestID string) (bool, error) {
	status, err := p.anyStatus(ctx, p.peers.galley, http.MethodGet, "/i/teams/"+team.String()+"/is-team-owner/"+user.String(), nil, requestID)
	return status != http.StatusForbidden, err
}

func (p accountDeletionPeers) remove(ctx context.Context, user storedUser, requestID string) error {
	// The captured pre-tombstone assets remain necessary after the row is cleared.
	// Validate keys before making any peer request; a malformed stored key cannot
	// become a path traversal or a request to an unrelated Cargohold endpoint.
	keys := make([]string, len(user.Assets))
	for i, asset := range user.Assets {
		key, valid := canonicalRegistrationAssetKey(asset.Key)
		if !valid {
			return errors.New("invalid deletion asset key")
		}
		keys[i] = key
	}
	for _, endpoint := range []Endpoint{p.peers.gundeck, p.peers.galley} {
		if _, _, err := p.peers.replayableRPCStatus(ctx, endpoint, http.MethodDelete, "/i/user", requestID, nil, false, false, &user.ID); err != nil {
			return err
		}
	}
	for _, key := range keys {
		// Native ignores HTTP errors for assets already deleted directly, including
		// 5xx. Only transport failure prevents the remaining account cleanup.
		if _, err := p.anyStatus(ctx, p.cargohold, http.MethodDelete, "/assets/v3/"+url.PathEscape(key), &user.ID, requestID); err != nil {
			return err
		}
	}
	return nil
}

// Requests without native expect2xx accept every HTTP status. Transport retries
// retain the native three retries with 100/200/400ms backoff. Response content is
// unused; draining is bounded so an untrusted peer cannot consume unbounded RAM.
func (p accountDeletionPeers) anyStatus(ctx context.Context, endpoint Endpoint, method, path string, user *gocql.UUID, requestID string) (int, error) {
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, "http://"+endpoint.Address()+path, nil)
		if err != nil {
			return 0, errors.New("invalid account deletion peer endpoint")
		}
		req.Header.Set("Request-Id", requestID)
		if user != nil {
			req.Header.Set("Z-User", user.String())
		}
		response, err := p.peers.client.Do(req)
		if err == nil {
			_, err = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<20))
			response.Body.Close()
			if err == nil {
				return response.StatusCode, nil
			}
		}
		if !retryableBrigRPC(err) || attempt >= 3 || ctx.Err() != nil {
			return 0, errors.New("account deletion peer request failed")
		}
		if waitBrigRetry(ctx, 100*time.Millisecond<<attempt) != nil {
			return 0, errors.New("account deletion peer request canceled")
		}
	}
}
