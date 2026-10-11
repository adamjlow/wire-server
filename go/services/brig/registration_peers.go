// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

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
	return p.joinRegistrationMember(ctx, user, team, nil, 3, requestID)
}

func (p brigPeers) joinRegistrationMember(ctx context.Context, user, team gocql.UUID, invitation *storedInvitation, role int32, requestID string) (bool, error) {
	if role < 1 || role > 4 {
		return false, errors.New("invalid invitation role")
	}
	mask := []uint64{8191, 5951, 1587, 1025}[role-1]
	var creator, created any
	if invitation != nil && invitation.CreatedBy != nil {
		creator = invitation.CreatedBy.String()
		created = invitation.CreatedAt.UTC().Format("2006-01-02T15:04:05.000Z")
	}
	raw, err := json.Marshal(map[string]any{"member": map[string]any{
		"user": user.String(), "permissions": map[string]uint64{"self": mask, "copy": mask}, "created_by": creator, "created_at": created,
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

// A valid Galley admission rejection is a native public error, not a storage
// failure. Keep its structured fields; never expose transport errors.
type registrationAdmissionError struct {
	Code    int             `json:"code"`
	Label   string          `json:"label"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
	Inner   json.RawMessage `json:"inner,omitempty"`
}

func (registrationAdmissionError) Error() string { return "team admission rejected" }

func (p brigPeers) checkRegistrationJoin(ctx context.Context, team gocql.UUID, requestID string) error {
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+p.galley.Address()+"/i/teams/"+team.String()+"/members/check", nil)
		if err != nil {
			return errors.New("invalid team admission endpoint")
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Request-Id", requestID)
		response, err := p.client.Do(req)
		if err != nil {
			if !retryableBrigRPC(err) || attempt >= 3 || waitBrigRetry(ctx, 100*time.Millisecond<<attempt) != nil {
				return errors.New("team admission request failed")
			}
			continue
		}
		raw, err := io.ReadAll(io.LimitReader(response.Body, 4<<20+1))
		response.Body.Close()
		if response.StatusCode == http.StatusOK {
			return nil
		}
		if err != nil || len(raw) > 4<<20 || !validRegistrationJSONText(raw) {
			return errors.New("invalid team admission response")
		}
		var required struct {
			Code           *int
			Label, Message *string
		}
		if json.Unmarshal(raw, &required) != nil || required.Code == nil || *required.Code < 100 || *required.Code > 999 || required.Label == nil || required.Message == nil {
			return errors.New("invalid team admission error")
		}
		var failure registrationAdmissionError
		if json.Unmarshal(bytes.TrimSpace(raw), &failure) != nil {
			return errors.New("invalid team admission error")
		}
		return failure
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
