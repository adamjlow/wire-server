// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"sync"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type sessionNotification struct {
	user      gocql.UUID
	requestID string
}
type sessionNotifications struct {
	ctx     context.Context
	cancel  context.CancelFunc
	pending chan sessionNotification
	workers sync.WaitGroup
}

// Native refresh suggestions are asynchronous best effort. Bound workers and
// queued work without exposing downstream failures or storing token material.
func newSessionNotifications(ctx context.Context, peer brigPeers) *sessionNotifications {
	workerContext, cancel := context.WithCancel(ctx)
	n := &sessionNotifications{ctx: workerContext, cancel: cancel, pending: make(chan sessionNotification, 256)}
	for range 4 {
		n.workers.Add(1)
		go func() {
			defer n.workers.Done()
			for {
				select {
				case <-n.ctx.Done():
					return
				case event := <-n.pending:
					if err := peer.sessionRefresh(n.ctx, event.user, event.requestID); err != nil {
						log.Print("session refresh notification failed")
					}
				}
			}
		}()
	}
	return n
}
func (n *sessionNotifications) close() { n.cancel(); n.workers.Wait() }
func (n *sessionNotifications) submit(ctx context.Context, user gocql.UUID, requestID string) error {
	select {
	case n.pending <- sessionNotification{user: user, requestID: requestID}:
	case <-ctx.Done():
		log.Print("session refresh notification canceled")
	case <-n.ctx.Done():
		log.Print("session refresh notification unavailable")
	}
	return nil
}

func (p brigPeers) sessionRefresh(ctx context.Context, user gocql.UUID, requestID string) error {
	body, err := json.Marshal([]any{map[string]any{
		"recipients": []any{map[string]any{"user_id": user.String(), "route": "direct", "clients": []string{}}},
		"origin":     user.String(), "payload": []any{map[string]any{"type": "user.session-refresh-suggested"}}, "is_cells_event": false,
	}})
	if err != nil {
		return errors.New("session refresh encoding failed")
	}
	// Native GundeckAPIAccess streams chunked JSON and explicitly does not retry.
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+p.gundeck.Address()+"/i/push/v2", io.NopCloser(bytes.NewReader(body)))
	if err != nil {
		return errors.New("invalid Gundeck endpoint")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Request-Id", requestID)
	response, err := p.client.Do(request)
	if err != nil {
		return errors.New("session refresh delivery failed")
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return errors.New("session refresh rejected")
	}
	return nil
}
