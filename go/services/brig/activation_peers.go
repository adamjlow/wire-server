// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func waitBrigRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func retryableBrigRPC(err error) bool {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var network *net.OpError
	var timeout net.Error
	return errors.As(err, &network) || errors.As(err, &timeout) && timeout.Timeout()
}

// Replayable Galley requests use native x3 (three retries, 100/200/400ms).
func (p brigPeers) activationGalley(ctx context.Context, method, path, requestID string, body []byte, allowMissing bool) ([]byte, bool, error) {
	return p.galleyRPC(ctx, method, path, requestID, body, allowMissing, nil)
}

func (p brigPeers) galleyRPC(ctx context.Context, method, path, requestID string, body []byte, allowMissing bool, user *gocql.UUID) ([]byte, bool, error) {
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, "http://"+p.galley.Address()+path, bytes.NewReader(body))
		if err != nil {
			return nil, false, errors.New("invalid Galley activation endpoint")
		}
		req.Header.Set("Request-Id", requestID)
		if user != nil {
			req.Header.Set("Z-User", user.String())
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		response, err := p.client.Do(req)
		retry := err != nil && retryableBrigRPC(err)
		if err == nil {
			raw, readErr := io.ReadAll(io.LimitReader(response.Body, 4<<20+1))
			response.Body.Close()
			if allowMissing && response.StatusCode == 404 {
				return nil, true, nil
			}
			if response.StatusCode >= 200 && response.StatusCode < 300 && (!allowMissing || response.StatusCode == 200) {
				if readErr == nil && len(raw) <= 4<<20 {
					return raw, false, nil
				}
				retry = readErr != nil && retryableBrigRPC(readErr)
			} else {
				retry = response.StatusCode >= 500 && response.StatusCode <= 599
			}
		}
		if !retry || attempt >= 3 || ctx.Err() != nil {
			return nil, false, errors.New("Galley activation request failed")
		}
		if waitBrigRetry(ctx, 100*time.Millisecond<<attempt) != nil {
			return nil, false, errors.New("Galley activation request canceled")
		}
	}
}
func (p brigPeers) activateBindingTeam(ctx context.Context, user gocql.UUID, requestID string) error {
	return p.activateBindingTeamCurrency(ctx, user, requestID, nil)
}
func (p brigPeers) activateBindingTeamCurrency(ctx context.Context, user gocql.UUID, requestID string, currency *string) error {
	raw, missing, err := p.activationGalley(ctx, http.MethodGet, "/i/users/"+user.String()+"/team", requestID, nil, true)
	if err != nil || missing {
		return err
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return errors.New("invalid Galley binding team")
	}
	team, err := gocql.ParseUUID(value)
	if err != nil {
		return errors.New("invalid Galley binding team")
	}
	body := map[string]string{"status": "active"}
	if currency != nil {
		body["currency"] = *currency
	}
	raw, err = json.Marshal(body)
	if err != nil {
		return errors.New("invalid team activation request")
	}
	_, _, err = p.activationGalley(ctx, http.MethodPut, "/i/teams/"+team.String()+"/status", requestID, raw, false)
	return err
}
