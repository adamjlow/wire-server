// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
)

type brigPeers struct {
	client *http.Client
	galley Endpoint
}

func (p brigPeers) hasE2EIdentity(ctx context.Context, user, requestID string) (bool, error) {
	endpoint := "http://" + p.galley.Address() + "/i/feature-configs?" + url.Values{"user_id": {user}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false, errors.New("invalid Galley endpoint")
	}
	req.Header.Set("Request-Id", requestID)
	response, err := p.client.Do(req)
	if err != nil {
		return false, errors.New("Galley feature lookup failed")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return false, errors.New("Galley feature lookup failed")
	}
	var features struct {
		E2EIdentity struct {
			Status string `json:"status"`
		} `json:"mlsE2EId"`
	}
	if json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&features) != nil {
		return false, errors.New("invalid Galley features")
	}
	switch features.E2EIdentity.Status {
	case "enabled":
		return true, nil
	case "disabled":
		return false, nil
	default:
		return false, errors.New("missing Galley E2E identity feature")
	}
}

func (p brigPeers) verificationEnabled(ctx context.Context, team, requestID string) (bool, error) {
	endpoint := "http://" + p.galley.Address() + "/i/teams/" + url.PathEscape(team) + "/features/sndFactorPasswordChallenge"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false, errors.New("invalid Galley endpoint")
	}
	req.Header.Set("Request-Id", requestID)
	response, err := p.client.Do(req)
	if err != nil {
		return false, errors.New("Galley verification feature lookup failed")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return false, errors.New("Galley verification feature lookup failed")
	}
	var feature struct {
		Status string `json:"status"`
	}
	if json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&feature) != nil {
		return false, errors.New("invalid Galley verification feature")
	}
	switch feature.Status {
	case "enabled":
		return true, nil
	case "disabled":
		return false, nil
	default:
		return false, errors.New("missing Galley verification feature status")
	}
}
