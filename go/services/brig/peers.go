// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"
)

type brigPeers struct {
	client  *http.Client
	galley  Endpoint
	gundeck Endpoint
}

func (p brigPeers) indexTeam(ctx context.Context, team, user, requestID string) (string, *indexRole, error) {
	get := func(path string, body []byte, output any) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+p.galley.Address()+path, bytes.NewReader(body))
		if err != nil {
			return errors.New("invalid Galley index endpoint")
		}
		req.Header.Set("Request-Id", requestID)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		response, err := p.client.Do(req)
		if err != nil {
			return errors.New("Galley index lookup failed")
		}
		defer response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return errors.New("Galley index lookup failed")
		}
		if json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(output) != nil {
			return errors.New("invalid Galley index response")
		}
		return nil
	}
	var feature struct {
		Status *string `json:"status"`
	}
	path := "/i/teams/" + url.PathEscape(team)
	if err := get(path+"/features/searchVisibilityInbound", nil, &feature); err != nil {
		return "", nil, err
	}
	visibility := "searchable-by-own-team"
	if feature.Status == nil {
		return "", nil, errors.New("missing Galley index feature")
	}
	switch *feature.Status {
	case "enabled":
		visibility = "searchable-by-all-teams"
	case "disabled":
	default:
		return "", nil, errors.New("invalid Galley index feature")
	}
	var list struct {
		Members *[]struct {
			UserID      *string                       `json:"userId"`
			Permissions *struct{ Self, Copy *uint64 } `json:"permissions"`
			Written     *time.Time                    `json:"permissionsWriteTime"`
		} `json:"members"`
	}
	body, _ := json.Marshal(map[string]any{"ids": []string{user}})
	if err := get(path+"/members/by-ids", body, &list); err != nil {
		return "", nil, err
	}
	if list.Members == nil {
		return "", nil, errors.New("missing Galley index members")
	}
	// Native decoder validates the entire list, then selects its first member.
	for _, member := range *list.Members {
		if member.UserID == nil || member.Permissions == nil || member.Written == nil || member.Permissions.Self == nil || member.Permissions.Copy == nil {
			return "", nil, errors.New("incomplete Galley index member")
		}
		if _, valid := tokenUUID(*member.UserID); !valid {
			return "", nil, errors.New("invalid Galley index member UUID")
		}
		// Unknown permission bits are discarded by native intToPerms. Copy must
		// be a subset after decoding, but role depends only on the self set.
		self, copy := *member.Permissions.Self&8191, *member.Permissions.Copy&8191
		if copy & ^self != 0 {
			return "", nil, errors.New("invalid Galley index permissions")
		}
	}
	if len(*list.Members) == 0 {
		return visibility, nil, nil
	}
	member := (*list.Members)[0]
	roles := map[uint64]string{8191: "owner", 5951: "admin", 1587: "member", 1025: "partner"}
	role, known := roles[*member.Permissions.Self&8191]
	if !known {
		return visibility, nil, nil
	}
	return visibility, &indexRole{name: role, written: member.Written.UTC().Truncate(time.Millisecond)}, nil
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
