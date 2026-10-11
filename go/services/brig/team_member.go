// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

// Decode the required native TeamMember, rather than interpreting an arbitrary
// 200 body as membership. Unknown permission bits are ignored by intToPerms.
func decodeTeamMember(raw []byte) (string, error) {
	failure := errors.New("invalid Galley team member")
	var object map[string]json.RawMessage
	var member struct{ User, Creator, Created, Legalhold *string }
	if !validRegistrationJSONText(raw) || json.Unmarshal(raw, &object) != nil || object == nil {
		return "", failure
	}
	// Aeson field names are case sensitive. Go's struct decoder alone would
	// accept User/Permissions/Self, which cannot establish native membership.
	for name, output := range map[string]any{"user": &member.User, "created_by": &member.Creator, "created_at": &member.Created, "legalhold_status": &member.Legalhold} {
		if field, present := object[name]; present && json.Unmarshal(field, output) != nil {
			return "", failure
		}
	}
	var permissions map[string]json.RawMessage
	if member.User == nil || json.Unmarshal(object["permissions"], &permissions) != nil || permissions == nil {
		return "", failure
	}
	if _, err := registrationUUID(*member.User); err != nil {
		return "", failure
	}
	self, err := teamPermissionWord(permissions["self"])
	if err != nil {
		return "", failure
	}
	copy, err := teamPermissionWord(permissions["copy"])
	if err != nil || (copy&8191)&^(self&8191) != 0 {
		return "", failure
	}
	if (member.Creator == nil) != (member.Created == nil) {
		return "", failure
	}
	if member.Creator != nil {
		if _, err := registrationUUID(*member.Creator); err != nil {
			return "", failure
		}
		if _, err := time.Parse(time.RFC3339Nano, *member.Created); err != nil {
			return "", failure
		}
	}
	if member.Legalhold == nil {
		return "no_consent", nil
	}
	switch *member.Legalhold {
	case "no_consent", "disabled", "pending", "enabled":
		return *member.Legalhold, nil
	default:
		return "", failure
	}
}

// Accept integral Scientific numbers without float rounding or allocating a
// power of ten from a hostile exponent. uint64 is the native permission type.
func teamPermissionWord(raw json.RawMessage) (uint64, error) {
	failure := errors.New("invalid team permissions")
	if !json.Valid(raw) {
		return 0, failure
	}
	m := registrationNumberPattern.FindStringSubmatch(string(bytes.TrimSpace(raw)))
	if m == nil {
		return 0, failure
	}
	digits := strings.TrimLeft(m[2]+m[3], "0")
	if digits == "" {
		return 0, nil
	}
	if m[1] == "-" {
		return 0, failure
	}
	exponent := 0
	if m[4] != "" {
		var err error
		exponent, err = strconv.Atoi(m[4])
		if err != nil || exponent > len(raw)+20 || exponent < -len(raw) {
			return 0, failure
		}
	}
	trimmed := strings.TrimRight(digits, "0")
	power := exponent - len(m[3]) + len(digits) - len(trimmed)
	if power < 0 || power > 20 || len(trimmed)+power > 20 {
		return 0, failure
	}
	n, err := strconv.ParseUint(trimmed+strings.Repeat("0", power), 10, 64)
	if err != nil {
		return 0, failure
	}
	return n, nil
}

func (p brigPeers) deletionTeamMember(ctx context.Context, user, team gocql.UUID, requestID string) (bool, error) {
	raw, missing, err := p.galleyRPC(ctx, http.MethodGet, "/i/teams/"+team.String()+"/members/"+user.String(), requestID, nil, true, &user)
	if err != nil || missing {
		return false, err
	}
	_, err = decodeTeamMember(raw)
	return err == nil, err
}
