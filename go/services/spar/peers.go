// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/apache/cassandra-gocql-driver/v2"
	"io"
	"net/http"
	"net/url"
)

type apiError struct {
	Code    int    `json:"code"`
	Label   string `json:"label"`
	Message string `json:"message"`
}

func (e *apiError) Error() string { return e.Label }
func domainError(w http.ResponseWriter, err error) {
	var e *apiError
	var ttl *ttlError
	if errors.As(err, &ttl) {
		err = &apiError{400, "ttl-error", ttl.Error()}
	}
	if !errors.As(err, &e) {
		e = &apiError{500, "server-error", "Server Error"}
	}
	w.Header()["Content-Type"] = nil
	w.WriteHeader(e.Code)
	_ = json.NewEncoder(w).Encode(e)
}

type peerClient struct {
	config Config
	http   *http.Client
}

func (p peerClient) request(ctx context.Context, endpoint Endpoint, method, path, requestID, user string, body any) (int, []byte, error) {
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return 0, nil, errors.New("peer request encoding failed")
		}
		payload = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://"+endpoint.Address()+path, payload)
	if err != nil {
		return 0, nil, errors.New("invalid peer request")
	}
	req.Header.Set("Request-Id", requestID)
	if user != "" {
		req.Header.Set("Z-User", user)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := p.http.Do(req)
	if err != nil {
		return 0, nil, errors.New("peer request failed")
	}
	defer response.Body.Close()
	encoded, err := io.ReadAll(response.Body)
	if err != nil {
		return 0, nil, errors.New("peer response failed")
	}
	return response.StatusCode, encoded, nil
}

// Hidden permissions use the highest native role contained in the intersection,
// not a claimed role field or a single powerful permission bit.
func canManageTokens(self, copy uint64) bool {
	const admin uint64 = 5951
	return (self & copy & admin) == admin
}
func (p peerClient) authorize(ctx context.Context, user, requestID string) (gocql.UUID, error) {
	if user == "" {
		return gocql.UUID{}, &apiError{400, "client-error", "[header] 'Z-User' required"}
	}
	uid, err := gocql.ParseUUID(user)
	if err != nil {
		return gocql.UUID{}, &apiError{400, "bad-request", "Invalid user ID"}
	}
	query := url.Values{"ids": {uid.String()}, "includePendingInvitations": {"false"}}
	status, body, err := p.request(ctx, p.config.Brig, "GET", "/i/users?"+query.Encode(), requestID, "", nil)
	if err != nil {
		return gocql.UUID{}, err
	}
	var accounts []struct {
		Team    *gocql.UUID `json:"team"`
		Deleted bool        `json:"deleted"`
	}
	if status == 404 {
		return gocql.UUID{}, &apiError{403, "no-team-member", "Requesting user is not a team member or not a member of this team."}
	}
	if status != 200 || json.Unmarshal(body, &accounts) != nil {
		return gocql.UUID{}, errors.New("invalid Brig account response")
	}
	if len(accounts) != 1 || accounts[0].Deleted || accounts[0].Team == nil {
		return gocql.UUID{}, &apiError{403, "no-team-member", "Requesting user is not a team member or not a member of this team."}
	}
	team := *accounts[0].Team
	status, body, err = p.request(ctx, p.config.Galley, "GET", fmt.Sprintf("/i/teams/%s/members/%s", team, uid), requestID, uid.String(), nil)
	if err != nil {
		return gocql.UUID{}, err
	}
	var member struct {
		Permissions struct {
			Self uint64 `json:"self"`
			Copy uint64 `json:"copy"`
		} `json:"permissions"`
	}
	if status != 200 && status != 404 {
		return gocql.UUID{}, errors.New("invalid Galley member response")
	}
	if status == 200 && json.Unmarshal(body, &member) != nil {
		return gocql.UUID{}, errors.New("invalid Galley member response")
	}
	if status == 200 && (member.Permissions.Copy&8191)&^(member.Permissions.Self&8191) != 0 {
		return gocql.UUID{}, errors.New("invalid Galley member permissions")
	}
	if status == 404 || !canManageTokens(member.Permissions.Self, member.Permissions.Copy) {
		return gocql.UUID{}, &apiError{403, "insufficient-permissions", "You need permission CreateReadDeleteScimToken."}
	}
	return team, nil
}
func (p peerClient) reauthenticate(ctx context.Context, user, requestID string, password, code *string) error {
	status, body, err := p.request(ctx, p.config.Brig, "GET", "/i/users/"+user+"/reauthenticate", requestID, "", map[string]any{"password": password, "verification_code": code, "action": "create_scim_token"})
	if err != nil {
		return err
	}
	if status == 200 {
		return nil
	}
	if status == 429 {
		return &apiError{429, "rate-limit-exceeded", "Please use exponential backoff throttling to mitigate this."}
	}
	if status != 403 {
		return errors.New("invalid Brig reauthentication response")
	}
	var upstream struct {
		Label string `json:"label"`
	}
	_ = json.Unmarshal(body, &upstream)
	switch upstream.Label {
	case "code-authentication-required":
		return &apiError{403, "code-authentication-required", "Reauthentication failed. Verification code required."}
	case "code-authentication-failed":
		return &apiError{403, "code-authentication-failed", "Reauthentication failed with invalid verification code."}
	default:
		return &apiError{403, "access-denied", "This operation requires reauthentication."}
	}
}
