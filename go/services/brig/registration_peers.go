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
