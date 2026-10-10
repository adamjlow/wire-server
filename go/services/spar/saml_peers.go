// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/apache/cassandra-gocql-driver/v2"
	"net/url"
)

// Brig alone creates SSO sessions. Keep its first Set-Cookie field separate
// from the response body and other headers; combining cookie fields corrupts
// Expires attributes. Verdict rendering parses this field before forwarding it.
func (p peerClient) ssoLogin(ctx context.Context, user gocql.UUID, label *string, requestID string) (string, error) {
	status, _, headers, err := p.requestWithHeaders(ctx, p.config.Brig, "POST", "/i/sso-login?persist=true", requestID, "", map[string]any{"user": user.String(), "label": label})
	if err != nil {
		return "", err
	}
	if status != 200 {
		return "", errors.New("invalid Brig SSO login response")
	}
	values := headers.Values("Set-Cookie")
	if len(values) == 0 {
		return "", errors.New("missing Brig SSO session cookie")
	}
	return values[0], nil
}

func samlSSOID(ref samlUserRef) map[string]string {
	return map[string]string{"tenant": encodedIssuer(ref.Issuer), "subject": ref.Subject.encoded()}
}

func (p peerClient) createSAMLAccount(ctx context.Context, uid, team gocql.UUID, ref samlUserRef, name, managedBy, role, requestID string) error {
	body := map[string]any{
		"newUserSparUUID": uid.String(), "newUserSparSSOId": samlSSOID(ref),
		"newUserSparDisplayName": name, "newUserSparTeamId": team.String(),
		"newUserSparManagedBy": managedBy, "newUserSparRole": role,
	}
	status, encoded, err := p.request(ctx, p.config.Brig, "POST", "/i/users/spar", requestID, "", body)
	if err != nil {
		return err
	}
	var profile struct {
		ID *gocql.UUID `json:"id"`
	}
	if (status != 200 && status != 201) || json.Unmarshal(encoded, &profile) != nil || profile.ID == nil || *profile.ID != uid {
		return errors.New("invalid Brig SAML account creation response")
	}
	return nil
}

func (p peerClient) setSAMLIdentity(ctx context.Context, uid gocql.UUID, ref samlUserRef, requestID string) error {
	status, _, err := p.request(ctx, p.config.Brig, "PUT", "/i/users/"+uid.String()+"/sso-id", requestID, "", samlSSOID(ref))
	if err != nil {
		return err
	}
	if status != 200 {
		return errors.New("Brig SAML identity update failed")
	}
	return nil
}

func (p peerClient) samlEmailValidation(ctx context.Context, team gocql.UUID, requestID string) (bool, error) {
	status, body, err := p.request(ctx, p.config.Galley, "GET", "/i/teams/"+team.String()+"/features/validateSAMLemails", requestID, "", nil)
	if err != nil {
		return false, err
	}
	var feature struct {
		Status string `json:"status"`
	}
	// Native feature lookup treats non-200 or malformed responses as disabled.
	return status == 200 && json.Unmarshal(body, &feature) == nil && feature.Status == "enabled", nil
}

func (p peerClient) updateSAMLEmail(ctx context.Context, uid gocql.UUID, email, requestID string, validate bool) error {
	q := url.Values{"activation": {"auto_activate"}, "validate": {"false"}, "activate": {"true"}}
	if validate {
		q.Set("activation", "send_activation_email")
		q.Set("validate", "true")
		q.Set("activate", "false")
	}
	status, _, err := p.request(ctx, p.config.Brig, "PUT", "/i/self/email?"+q.Encode(), requestID, uid.String(), map[string]string{"email": email})
	if err != nil {
		return err
	}
	if status != 202 && status != 204 {
		return errors.New("Brig SAML email update failed")
	}
	return nil
}
