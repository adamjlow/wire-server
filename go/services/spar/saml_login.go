// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"context"
	"reflect"
	"unicode/utf8"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type samlLoginStore interface {
	samlUser(context.Context, samlUserRef) (*gocql.UUID, error)
	insertSAMLUser(context.Context, samlUserRef, gocql.UUID) error
	deleteSAMLUser(context.Context, gocql.UUID, samlUserRef) error
	teamTokens(context.Context, gocql.UUID) ([]tokenInfo, error)
	idpsByTeam(context.Context, gocql.UUID) ([]idpConfig, error)
}
type samlLoginPeers interface {
	account(context.Context, gocql.UUID, string, bool) (*brigAccount, error)
	createSAMLAccount(context.Context, gocql.UUID, gocql.UUID, samlUserRef, string, string, string, string) error
	setSAMLIdentity(context.Context, gocql.UUID, samlUserRef, string) error
	samlEmailValidation(context.Context, gocql.UUID, string) (bool, error)
	updateSAMLEmail(context.Context, gocql.UUID, string, string, bool) error
	ssoLogin(context.Context, gocql.UUID, *string, string) (string, error)
}

// Mapping lookup must consult Brig: a deleted account permits re-provisioning,
// but a surviving account in another team must never be adopted or skipped.
func mappedSAMLAccount(ctx context.Context, s samlLoginStore, p samlLoginPeers, ref samlUserRef, requestID string) (*brigAccount, error) {
	uid, err := s.samlUser(ctx, ref)
	if err != nil || uid == nil {
		return nil, err
	}
	return p.account(ctx, *uid, requestID, true)
}

func moveSAMLIdentity(ctx context.Context, s samlLoginStore, p samlLoginPeers, uid gocql.UUID, old, next samlUserRef, requestID string) error {
	if err := s.insertSAMLUser(ctx, next, uid); err != nil {
		return err
	}
	if err := p.setSAMLIdentity(ctx, uid, next, requestID); err != nil {
		return err
	}
	return s.deleteSAMLUser(ctx, uid, old)
}

func findSAMLAccount(ctx context.Context, s samlLoginStore, p samlLoginPeers, idp idpConfig, ref samlUserRef, requestID string) (*gocql.UUID, error) {
	refs := []samlUserRef{ref}
	for _, issuer := range idp.Extra.OldIssuers {
		refs = append(refs, samlUserRef{issuer, ref.Subject})
	}
	for i, candidate := range refs {
		account, err := mappedSAMLAccount(ctx, s, p, candidate, requestID)
		if err != nil {
			return nil, err
		}
		if account == nil {
			continue
		}
		if account.Team == nil || *account.Team != idp.Extra.Team {
			// Exact native Show UserRef detail is completed with verdict rendering.
			return nil, &apiError{403, "bad-team", "Forbidden: multiple teams or no team for same UserRef " + encodedIssuer(ref.Issuer) + ref.Subject.encoded()}
		}
		if i != 0 {
			if err = moveSAMLIdentity(ctx, s, p, account.ID, candidate, ref, requestID); err != nil {
				return nil, err
			}
		}
		return &account.ID, nil
	}
	return nil, nil
}

func provisionSAMLAccount(ctx context.Context, s samlLoginStore, p samlLoginPeers, idp idpConfig, ref samlUserRef, requestID string) (gocql.UUID, error) {
	var zero gocql.UUID
	if idp.Extra.ReplacedBy != nil {
		return zero, &apiError{400, "cannont-provision-on-replaced-idp", "This IdP has been replaced, users can only be auto-provisioned on the replacing IdP " + idp.ID.String()}
	}
	tokens, err := s.teamTokens(ctx, idp.Extra.Team)
	if err != nil {
		return zero, err
	}
	if len(tokens) != 0 {
		return zero, &apiError{404, "not-found", "Could not find SAML credentials, and auto-provisioning is disabled."}
	}
	name := ref.Subject.Value
	if n := utf8.RuneCountInString(name); n < 1 || n > 128 {
		return zero, &apiError{400, "bad-username", "Bad UserName in SAML response, except len [1, 128]: Name: outside range [1, 128]"}
	}
	uid, err := gocql.RandomUUID()
	if err != nil {
		return zero, err
	}
	// Actual native order creates Brig first, then inserts the mapping.
	if err = p.createSAMLAccount(ctx, uid, idp.Extra.Team, ref, name, "wire", "member", requestID); err != nil {
		return zero, err
	}
	if err = s.insertSAMLUser(ctx, ref, uid); err != nil {
		return zero, err
	}
	if ref.Subject.Format == nameID11+"emailAddress" {
		account, err := p.account(ctx, uid, requestID, false)
		if err != nil {
			return zero, err
		}
		if account != nil && account.Team != nil {
			validate, err := p.samlEmailValidation(ctx, *account.Team, requestID)
			if err != nil {
				return zero, err
			}
			if err = p.updateSAMLEmail(ctx, uid, name, requestID, validate); err != nil {
				return zero, err
			}
		}
	}
	return uid, nil
}

func samlLoginUser(ctx context.Context, c Config, s samlLoginStore, p samlLoginPeers, idp idpConfig, ref samlUserRef, host *string, label *string, requestID string) (gocql.UUID, string, error) {
	var zero gocql.UUID
	uid, err := findSAMLAccount(ctx, s, p, idp, ref, requestID)
	if err != nil {
		return zero, "", err
	}
	if uid == nil && c.SAML.Domains != nil {
		if ref.Subject.Format != nameID11+"emailAddress" {
			return zero, "", &apiError{400, "multi-ingress-config-error", "Multi-ingress SSO only supports email-based NameIDs for cross-IdP migration. Username-based NameIDs are not allowed."}
		}
		idps, err := s.idpsByTeam(ctx, idp.Extra.Team)
		if err != nil {
			return zero, "", err
		}
		var authenticating *idpConfig
		for i := range idps {
			if idps[i].Metadata.Issuer == ref.Issuer && reflect.DeepEqual(idps[i].Extra.Domain, host) {
				authenticating = &idps[i]
				break
			}
		}
		if authenticating == nil {
			domain := "default"
			if host != nil {
				domain = *host
			}
			return zero, "", &apiError{404, "not-found", "Could not find IdP: IdP with issuer '" + ref.Issuer + "' for domain '" + domain + "' is not configured for this team"}
		}
		if err = checkIdPCertificates(c, authenticating.Metadata); err != nil {
			return zero, "", err
		}
		for _, candidate := range idps {
			old := samlUserRef{candidate.Metadata.Issuer, ref.Subject}
			uid, err = findSAMLAccount(ctx, s, p, candidate, old, requestID)
			if err != nil {
				return zero, "", err
			}
			if uid != nil {
				if err = moveSAMLIdentity(ctx, s, p, *uid, old, ref, requestID); err != nil {
					return zero, "", err
				}
				break
			}
		}
	}
	if uid == nil {
		newUID, err := provisionSAMLAccount(ctx, s, p, idp, ref, requestID)
		if err != nil {
			return zero, "", err
		}
		uid = &newUID
	}
	cookie, err := p.ssoLogin(ctx, *uid, label, requestID)
	if err != nil {
		return zero, "", err
	}
	return *uid, cookie, nil
}
