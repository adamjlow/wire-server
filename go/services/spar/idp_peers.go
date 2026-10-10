// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/apache/cassandra-gocql-driver/v2"
)

func permissionError(permission string) error {
	return &apiError{403, "insufficient-permissions", "You need permission " + permission + "."}
}
func (p peerClient) idpPermission(ctx context.Context, user, requestID string, team gocql.UUID) error {
	if user == "" {
		return permissionError("CreateUpdateDeleteIdp")
	}
	uid, err := gocql.ParseUUID(user)
	if err != nil {
		return permissionError("CreateUpdateDeleteIdp")
	}
	status, body, err := p.request(ctx, p.config.Galley, "GET", fmt.Sprintf("/i/teams/%s/members/%s", team, uid), requestID, uid.String(), nil)
	if err != nil {
		return err
	}
	var member struct {
		Permissions struct {
			Self uint64 `json:"self"`
			Copy uint64 `json:"copy"`
		} `json:"permissions"`
	}
	if status == 404 {
		return permissionError("CreateUpdateDeleteIdp")
	}
	if status != 200 || json.Unmarshal(body, &member) != nil || (member.Permissions.Copy&8191)&^(member.Permissions.Self&8191) != 0 {
		return errors.New("invalid Galley member response")
	}
	if !canManageTokens(member.Permissions.Self, member.Permissions.Copy) {
		return permissionError("CreateUpdateDeleteIdp")
	}
	return nil
}
func (p peerClient) idpAdmin(ctx context.Context, user, requestID string) (gocql.UUID, error) {
	if user == "" {
		return gocql.UUID{}, &apiError{400, "client-error", "[header] 'Z-User' required"}
	}
	uid, err := gocql.ParseUUID(user)
	if err != nil {
		return gocql.UUID{}, permissionError("[internal error] Can't parse Z-User header")
	}
	status, body, err := p.request(ctx, p.config.Brig, "GET", "/i/users/"+uid.String()+"/check-admin-get-team-id", requestID, "", nil)
	if err != nil {
		return gocql.UUID{}, err
	}
	if status == 403 {
		var upstream apiError
		if json.Unmarshal(body, &upstream) != nil {
			return gocql.UUID{}, errors.New("invalid Brig admin response")
		}
		return gocql.UUID{}, permissionError(upstream.Message)
	}
	var team gocql.UUID
	if status != 200 || json.Unmarshal(body, &team) != nil {
		return gocql.UUID{}, errors.New("invalid Brig admin response")
	}
	return team, nil
}
func (p peerClient) ssoEnabled(ctx context.Context, team gocql.UUID, requestID string) error {
	status, body, err := p.request(ctx, p.config.Galley, "GET", "/i/teams/"+team.String()+"/features/sso", requestID, "", nil)
	if err != nil {
		return err
	}
	var feature struct {
		Status string `json:"status"`
	}
	if status != 200 || json.Unmarshal(body, &feature) != nil {
		return errors.New("invalid Galley feature response")
	}
	if feature.Status != "enabled" {
		return &apiError{403, "sso-disabled", "Please ask customer support to enable this feature for your team."}
	}
	return nil
}
func (p peerClient) notifyIdP(ctx context.Context, tag, user, requestID string, old, new *idpConfig) error {
	value := map[string]any{}
	if user != "" {
		value["user"] = user
	}
	if tag == "updated" {
		value["old"] = old
		value["new"] = new
	} else {
		value["idp"] = new
	}
	status, _, err := p.request(ctx, p.config.Brig, "POST", "/i/idp/send-idp-changed-email", requestID, "", map[string]any{"tag": tag, "value": value})
	if err != nil {
		return err
	}
	if status != 200 {
		return errors.New("Brig IdP notification failed")
	}
	return nil
}
