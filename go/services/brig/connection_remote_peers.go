// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/apache/cassandra-gocql-driver/v2"
	"net/http"
)

func (s federationConfigRead) connectionAllowed(ctx context.Context, domain string, team *gocql.UUID, session *gocql.Session) (bool, error) {
	switch s.strategy {
	case "allowNone":
		return false, nil
	case "allowAll":
		return true, nil
	case "allowDynamic":
	default:
		return false, errors.New("invalid federation strategy")
	}
	for _, configured := range s.static {
		if configured.Domain == domain {
			return true, nil
		}
	}
	var search *int32
	var restriction *int32
	err := session.Query(`SELECT search_policy, restriction FROM federation_remotes WHERE domain = ?`, domain).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).Scan(&search, &restriction)
	if errors.Is(err, gocql.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if _, _, err := federationSearchPolicy(&domain, search); err != nil {
		return false, err
	}
	if restriction == nil || *restriction == 0 {
		return true, nil
	}
	if *restriction != 1 {
		return false, errors.New("invalid federation restriction")
	}
	// Native resolves the restriction's team rows even for a caller with no
	// team. Missing team must never acquire permission for restrict_by_team.
	teams, err := s.teams(ctx, domain)
	if err != nil {
		return false, err
	}
	if team == nil {
		return false, nil
	}
	for _, allowed := range teams {
		if allowed == team.String() {
			return true, nil
		}
	}
	return false, nil
}
func (p brigPeers) upsertConnectionConversation(ctx context.Context, self, target gocql.UUID, domain string, conv gocql.UUID, convDomain string, remote, included bool, requestID, ownDomain string) error {
	actor := "local_actor"
	if remote {
		actor = "remote_actor"
	}
	membership := "excluded"
	if included {
		membership = "included"
	}
	body, err := json.Marshal(map[string]any{"local_user": qualifiedUserID{self.String(), ownDomain}, "remote_user": qualifiedUserID{target.String(), domain}, "conversation_id": qualifiedUserID{conv.String(), convDomain}, "actor": actor, "actor_desired_membership": membership})
	if err != nil {
		return err
	}
	_, status, err := p.galleyRPCStatus(ctx, http.MethodPost, "/i/conversations/one2one/upsert", requestID, body, false, false, nil)
	if err != nil {
		return err
	}
	if status != 200 {
		return errors.New("unexpected remote conversation upsert status")
	}
	return nil
}
