// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type internalCleanupStore interface {
	deleteTeamTokens(context.Context, gocql.UUID) error
	idpsByTeam(context.Context, gocql.UUID) ([]idpConfig, error)
	deleteSAMLUsersByIssuer(context.Context, string) error
	deleteIdPConfig(context.Context, idpConfig) error
	deleteSAMLUser(context.Context, gocql.UUID, samlUserRef) error
	deleteSCIMExternal(context.Context, gocql.UUID, string) error
	deleteSCIMMeta(context.Context, gocql.UUID) error
}

func (s cassandraStore) deleteTeamTokens(ctx context.Context, team gocql.UUID) error {
	iter := s.read(ctx, "SELECT token_ FROM team_provisioning_by_team WHERE team = ?", team).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5}).Iter()
	keys := []string{}
	var key *string
	for iter.Scan(&key) {
		if key == nil {
			_ = iter.Close()
			return errors.New("invalid stored SCIM token lookup key")
		}
		keys = append(keys, *key)
	}
	if err := iter.Close(); err != nil {
		return err
	}
	b := s.batch(ctx)
	b.Query("DELETE FROM team_provisioning_by_team WHERE team = ?", team)
	for _, key := range keys {
		b.Query("DELETE FROM team_provisioning_by_token WHERE token_ = ?", key)
	}
	return s.session.ExecuteBatch(b)
}

func cleanupTeam(ctx context.Context, s internalCleanupStore, team gocql.UUID) error {
	if err := s.deleteTeamTokens(ctx, team); err != nil {
		return err
	}
	idps, err := s.idpsByTeam(ctx, team)
	if err != nil {
		return err
	}
	for _, idp := range idps {
		if err := s.deleteSAMLUsersByIssuer(ctx, idp.Metadata.Issuer); err != nil {
			return err
		}
		if err := s.deleteIdPConfig(ctx, idp); err != nil {
			return err
		}
	}
	return nil
}

func cleanupPendingSCIMUser(ctx context.Context, s internalCleanupStore, p peerClient, team, uid gocql.UUID, requestID string) error {
	account, err := p.account(ctx, uid, requestID, true)
	if err != nil || account == nil {
		return err
	}
	if account.Team == nil || *account.Team != team || account.ManagedBy != "scim" || account.Status != "pending-invitation" {
		return nil
	}
	identity, err := scimAccountIdentity(*account, nil, true)
	if err != nil {
		return err
	}
	if identity != nil {
		if identity.Ref != nil {
			if err := s.deleteSAMLUser(ctx, account.ID, *identity.Ref); err != nil {
				return err
			}
		}
		if err := s.deleteSCIMExternal(ctx, team, identity.External); err != nil {
			return err
		}
	}
	return s.deleteSCIMMeta(ctx, account.ID)
}

func serveInternalCleanup(w http.ResponseWriter, r *http.Request, s internalCleanupStore, p peerClient) bool {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
	teamRoute := len(parts) == 3 && parts[0] == "i" && parts[1] == "teams"
	userRoute := len(parts) == 5 && parts[0] == "i" && parts[1] == "scim" && parts[2] == "users"
	if !teamRoute && !userRoute {
		return false
	}
	teamText := parts[2]
	if userRoute {
		teamText = parts[3]
	}
	team, err := gocql.ParseUUID(teamText)
	var uid gocql.UUID
	if err == nil && userRoute {
		uid, err = gocql.ParseUUID(parts[4])
	}
	if err != nil {
		domainError(w, &apiError{400, "bad-request", "Invalid team or user ID"})
		return true
	}
	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return true
	}
	if teamRoute {
		err = cleanupTeam(r.Context(), s, team)
	} else {
		err = cleanupPendingSCIMUser(r.Context(), s, p, team, uid, r.Header.Get("Request-Id"))
	}
	if err != nil {
		domainError(w, err)
		return true
	}
	w.WriteHeader(http.StatusNoContent)
	return true
}
