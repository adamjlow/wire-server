// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func registrationGalleyVersion(c Config) (int, error) {
	for version := 19; version >= 0; version-- {
		if !disabledVersion(c, version) {
			return version, nil
		}
	}
	return 0, errors.New("no available Galley API version")
}
func (p brigPeers) createSelfConversation(ctx context.Context, user gocql.UUID, version int, requestID string) error {
	_, _, err := p.galleyRPC(ctx, http.MethodPost, "/v"+strconv.Itoa(version)+"/conversations/self", requestID, nil, false, &user)
	return err
}
func (p brigPeers) createRegistrationTeam(ctx context.Context, user, team gocql.UUID, input registrationTeam, requestID string) error {
	// BindingNewTeamUser currency is used during activation, not team creation.
	body := map[string]any{"name": input.Name, "icon": input.Icon}
	if input.IconKey != nil {
		body["icon_key"] = *input.IconKey
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return errors.New("team creation encoding failed")
	}
	_, _, err = p.galleyRPC(ctx, http.MethodPut, "/i/teams/"+team.String(), requestID, raw, false, &user)
	return err
}

// SSO join uses native defaultRole (member): identical self/copy masks. Null
// invitation metadata is part of NewTeamMember's original JSON contract.
func (p brigPeers) joinRegistrationSSO(ctx context.Context, user, team gocql.UUID, requestID string) (bool, error) {
	raw, err := json.Marshal(map[string]any{"member": map[string]any{
		"user": user.String(), "permissions": map[string]uint64{"self": 1587, "copy": 1587}, "created_by": nil, "created_at": nil,
	}})
	if err != nil {
		return false, errors.New("team member encoding failed")
	}
	_, status, err := p.galleyRPCStatus(ctx, http.MethodPost, "/i/teams/"+team.String()+"/members", requestID, raw, false, true, &user)
	if err != nil {
		return false, err
	}
	switch status {
	case http.StatusOK:
		return true, nil
	case http.StatusForbidden:
		return false, nil
	default:
		return false, errors.New("invalid team membership response")
	}
}

func (p brigPeers) registrationTeamName(ctx context.Context, team gocql.UUID, requestID string) (string, error) {
	raw, _, err := p.activationGalley(ctx, http.MethodGet, "/i/teams/"+team.String()+"/name", requestID, nil, false)
	if err != nil {
		return "", err
	}
	var value struct {
		Name *string `json:"name"`
	}
	if !validRegistrationJSONText(raw) || json.Unmarshal(raw, &value) != nil || value.Name == nil {
		return "", errors.New("invalid Galley team name")
	}
	return *value.Name, nil
}
