// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"context"
	"errors"
	"github.com/apache/cassandra-gocql-driver/v2"
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
