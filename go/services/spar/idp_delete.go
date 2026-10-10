// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"net/http"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type brigAccount struct {
	ID      gocql.UUID                         `json:"id"`
	Team    *gocql.UUID                        `json:"team"`
	Deleted bool                               `json:"deleted"`
	SSOID   *struct{ Tenant, Subject *string } `json:"sso_id"`
}

func (p peerClient) account(ctx context.Context, uid gocql.UUID, requestID string, pending bool) (*brigAccount, error) {
	include := "false"
	if pending {
		include = "true"
	}
	status, body, err := p.request(ctx, p.config.Brig, "GET", "/i/users?ids="+uid.String()+"&includePendingInvitations="+include, requestID, "", nil)
	if err != nil {
		return nil, err
	}
	if status == 404 {
		return nil, nil
	}
	var accounts []brigAccount
	if status != 200 || json.Unmarshal(body, &accounts) != nil {
		return nil, errors.New("invalid Brig account response")
	}
	if len(accounts) != 1 || accounts[0].Deleted {
		return nil, nil
	}
	return &accounts[0], nil
}

func (a *brigAccount) samlIssuer() (*string, error) {
	if a == nil || a.SSOID == nil || a.SSOID.Tenant == nil || a.SSOID.Subject == nil {
		return nil, nil
	}
	node, err := parseXMLNode([]byte(*a.SSOID.Tenant))
	if err != nil || node.Name != (xml.Name{Space: assertionNS, Local: "Issuer"}) || len(node.Children) != 0 || !absoluteURI(node.Text) {
		return nil, errors.New("invalid Brig SAML identity")
	}
	issuer := node.Text
	return &issuer, nil
}

func (p peerClient) deleteAccount(ctx context.Context, uid gocql.UUID, requestID string) error {
	status, _, err := p.request(ctx, p.config.Brig, "DELETE", "/i/users/"+uid.String(), requestID, "", nil)
	if err != nil {
		return err
	}
	if (status < 200 || status >= 300) && status != 404 {
		return errors.New("Brig account deletion failed")
	}
	return nil
}

func deleteIdP(ctx context.Context, c Config, s cassandraStore, p peerClient, info idpConfig, user, requestID string, purge bool) error {
	if err := p.idpPermission(ctx, user, requestID, info.Extra.Team); err != nil {
		return err
	}
	uid, err := gocql.ParseUUID(user)
	if err != nil {
		return permissionError("CreateUpdateDeleteIdp")
	}
	account, err := p.account(ctx, uid, requestID, false)
	if err != nil {
		return err
	}
	issuer, err := account.samlIssuer()
	if err != nil {
		return err
	}
	if issuer != nil && *issuer == info.Metadata.Issuer {
		return &apiError{409, "cannot-delete-own-idp", "You cannot delete the IdP used to login with your own account."}
	}
	var page []byte
	for {
		users, next, err := s.samlUsersPage(ctx, info.Metadata.Issuer, page)
		if err != nil {
			return err
		}
		for _, mapped := range users {
			account, err := p.account(ctx, mapped.UID, requestID, false)
			if err != nil {
				return err
			}
			// user_v2 has no team column: querying Brig is necessary to avoid
			// purging another team's users sharing a version-2 issuer.
			if account == nil || account.Team == nil || *account.Team != info.Extra.Team {
				continue
			}
			if !purge {
				return &apiError{412, "idp-has-bound-users", "an idp can only be deleted if it is empty"}
			}
			if err = s.deleteSAMLUser(ctx, mapped.UID, mapped.Ref); err != nil {
				return err
			}
			if err = p.deleteAccount(ctx, mapped.UID, requestID); err != nil {
				return err
			}
		}
		if len(next) == 0 {
			break
		}
		page = next
	}
	for _, previous := range info.Extra.OldIssuers {
		old, err := s.idpByIssuer(ctx, previous, info.version(), info.Extra.Team)
		if err != nil {
			return err
		}
		if old != nil {
			if err = s.setIdPReplacement(ctx, old.ID, nil); err != nil {
				return err
			}
		}
	}
	tokens, err := s.teamTokens(ctx, info.Extra.Team)
	if err != nil {
		return err
	}
	for _, token := range tokens {
		if token.IdP != nil && *token.IdP == info.ID {
			if err = s.deleteToken(ctx, info.Extra.Team, token.ID); err != nil {
				return err
			}
		}
	}
	if err = s.deleteIdPConfig(ctx, info); err != nil {
		return err
	}
	if c.SAML.Domains != nil {
		return p.notifyIdP(ctx, "deleted", user, requestID, nil, &info)
	}
	return nil
}

func serveIdPDelete(w http.ResponseWriter, r *http.Request, c Config, s cassandraStore, p peerClient, id gocql.UUID) {
	purge := false
	if values, ok := r.URL.Query()["purge"]; ok && len(values) > 0 {
		switch values[0] {
		case "true":
			purge = true
		case "false":
		default:
			domainError(w, &apiError{400, "bad-request", "Invalid purge parameter"})
			return
		}
	}
	info, err := s.getIdP(r.Context(), id)
	if err == nil {
		err = deleteIdP(r.Context(), c, s, p, *info, r.Header.Get("Z-User"), r.Header.Get("Request-Id"), purge)
	}
	if err != nil {
		domainError(w, idpError(err))
		return
	}
	w.WriteHeader(204)
}
