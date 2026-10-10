// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/url"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func richAssoc(r richInfo) map[string]any {
	return map[string]any{"version": 0, "fields": r.normalized()}
}

func (p peerClient) scimAccountField(ctx context.Context, uid gocql.UUID, field string, value any, requestID string) error {
	status, body, err := p.request(ctx, p.config.Brig, "PUT", "/i/users/"+uid.String()+"/"+field, requestID, "", value)
	if err != nil {
		return err
	}
	var upstream struct {
		Label *string `json:"label"`
	}
	_ = json.Unmarshal(body, &upstream)
	if (field == "name" && status < 300) || (status == 200 && (field != "handle" || upstream.Label == nil)) {
		return nil
	}
	return errors.New("Brig SCIM account update failed")
}

func (p peerClient) scimRichInfo(ctx context.Context, uid gocql.UUID, requestID string) (richInfo, error) {
	status, body, err := p.request(ctx, p.config.Brig, "GET", "/i/users/"+uid.String()+"/rich-info", requestID, "", nil)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var object map[string]any
	if status != 200 || decoder.Decode(&object) != nil {
		return nil, errors.New("invalid Brig rich info response")
	}
	return parseRichInfo(object)
}

func (p peerClient) scimHandleAvailable(ctx context.Context, handle, requestID string) (bool, error) {
	status, _, err := p.request(ctx, p.config.Brig, "HEAD", "/i/users/handles/"+url.PathEscape(handle), requestID, "", nil)
	if err != nil {
		return false, err
	}
	if status == 200 || status == 404 {
		return status == 404, nil
	}
	return false, errors.New("invalid Brig handle availability response")
}

func validAccountStatus(status string) bool {
	return status == "active" || status == "suspended" || status == "deleted" || status == "ephemeral" || status == "pending-invitation"
}

func (p peerClient) scimAccountStatus(ctx context.Context, uid gocql.UUID, requestID string) (*string, error) {
	status, body, err := p.request(ctx, p.config.Brig, "GET", "/i/users/"+uid.String()+"/status", requestID, "", nil)
	if err != nil || status == 404 {
		return nil, err
	}
	var result struct {
		Status *string `json:"status"`
	}
	if status != 200 || json.Unmarshal(body, &result) != nil || result.Status == nil || !validAccountStatus(*result.Status) {
		return nil, errors.New("invalid Brig account status response")
	}
	return result.Status, nil
}

func (p peerClient) scimRole(ctx context.Context, uid, team gocql.UUID, requestID string) (string, error) {
	status, body, err := p.request(ctx, p.config.Galley, "GET", "/i/teams/"+team.String()+"/members/"+uid.String(), requestID, "", nil)
	if err != nil {
		return "", err
	}
	if status == 404 {
		return "member", nil
	}
	var member struct {
		Permissions *struct{ Self, Copy uint64 } `json:"permissions"`
	}
	if status != 200 || json.Unmarshal(body, &member) != nil || member.Permissions == nil {
		return "", errors.New("invalid Galley member response")
	}
	bits := member.Permissions.Self & member.Permissions.Copy & 8191
	for _, role := range []struct {
		Name string
		Bits uint64
	}{{"owner", 8191}, {"admin", 5951}, {"member", 1587}, {"partner", 1025}} {
		if bits&role.Bits == role.Bits {
			return role.Name, nil
		}
	}
	return "member", nil
}

func (p peerClient) scimSetManaged(ctx context.Context, uid gocql.UUID, requestID string) error {
	if err := p.scimAccountField(ctx, uid, "managed-by", map[string]string{"managed_by": "scim"}, requestID); err != nil {
		return err
	}
	status, _, err := p.request(ctx, p.config.Brig, "DELETE", "/i/users/"+uid.String()+"/pending-email-update", requestID, "", nil)
	if err != nil {
		return err
	}
	if status != 200 {
		return errors.New("Brig pending email update deletion failed")
	}
	return nil
}

func (p peerClient) createSCIMAccount(ctx context.Context, uid, team gocql.UUID, user validSCIMUser, requestID string) error {
	role := "member"
	if user.Role != nil {
		role = *user.Role
	}
	var body map[string]any
	path := "/i/users/spar"
	if user.Ref != nil {
		body = map[string]any{"newUserSparUUID": uid.String(), "newUserSparSSOId": samlSSOID(*user.Ref), "newUserSparDisplayName": user.Name, "newUserSparTeamId": team.String(), "newUserSparManagedBy": "scim", "newUserSparRole": role, "newUserSparHandle": user.Handle, "newUserSparRichInfo": user.RichInfo.object()}
		if user.Language != nil {
			body["newUserSparLocale"] = *user.Language
		}
	} else {
		if user.Email == nil {
			return errors.New("SCIM invitation requires email")
		}
		path = "/i/teams/" + team.String() + "/invitations"
		body = map[string]any{"team_id": team.String(), "user_id": uid.String(), "external_id": user.External, "name": user.Name, "email": user.Email.Value, "role": role}
		if user.Language != nil {
			body["locale"] = *user.Language
		}
	}
	status, encoded, err := p.request(ctx, p.config.Brig, "POST", path, requestID, "", body)
	if err != nil {
		return err
	}
	var result struct {
		ID *gocql.UUID `json:"id"`
	}
	if (status != 200 && status != 201) || json.Unmarshal(encoded, &result) != nil || result.ID == nil {
		return errors.New("invalid Brig SCIM account creation response")
	}
	if user.Ref == nil {
		return p.scimAccountField(ctx, uid, "handle", map[string]string{"handle": user.Handle}, requestID)
	}
	return nil
}

func (p peerClient) scimAccountSearch(ctx context.Context, field, value, requestID string) (*brigAccount, error) {
	q := url.Values{field: {value}, "includePendingInvitations": {"true"}}
	status, body, err := p.request(ctx, p.config.Brig, "GET", "/i/users?"+q.Encode(), requestID, "", nil)
	if err != nil || status == 404 {
		return nil, err
	}
	var users []brigAccount
	if status != 200 || json.Unmarshal(body, &users) != nil {
		return nil, errors.New("invalid Brig account search response")
	}
	if len(users) == 0 {
		return nil, nil
	}
	if field == "email" && (users[0].Email == nil || *users[0].Email != value) {
		return nil, nil
	}
	return &users[0], nil
}
