// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type scimUserStore interface {
	scimTokenLookup
	getIdP(context.Context, gocql.UUID) (*idpConfig, error)
	samlUser(context.Context, samlUserRef) (*gocql.UUID, error)
	insertSAMLUser(context.Context, samlUserRef, gocql.UUID) error
	deleteSAMLUser(context.Context, gocql.UUID, samlUserRef) error
	scimExternal(context.Context, gocql.UUID, string) (*gocql.UUID, error)
	scimCreation(context.Context, gocql.UUID, string) (*scimCreation, error)
	insertSCIMExternal(context.Context, gocql.UUID, string, gocql.UUID) error
	setSCIMCreation(context.Context, gocql.UUID, string, gocql.UUID, bool) error
	deleteSCIMExternal(context.Context, gocql.UUID, string) error
	scimMeta(context.Context, gocql.UUID) (*scimUserMeta, error)
	writeSCIMMeta(context.Context, gocql.UUID, scimUserMeta) error
	deleteSCIMMeta(context.Context, gocql.UUID) error
}

type scimIdentity struct {
	External string
	Email    *string
	Ref      *samlUserRef
}

func scimAccountIdentity(a brigAccount, idp *idpConfig, old bool) (*scimIdentity, error) {
	email := a.Email
	if email == nil {
		email = a.UnvalidatedEmail
	}
	identity := &scimIdentity{Email: email}
	if a.SSOID != nil {
		if a.SSOID.External != nil {
			identity.External = *a.SSOID.External
		} else {
			issuer, err := a.samlIssuer()
			if err != nil || issuer == nil || a.SSOID.Subject == nil {
				return nil, errors.New("invalid Brig SCIM identity")
			}
			subject, err := parseNameID(*a.SSOID.Subject)
			if err != nil {
				return nil, err
			}
			identity.External = subject.Value
			if old || idp != nil {
				identity.Ref = &samlUserRef{*issuer, subject}
			}
		}
	} else {
		if old || email == nil {
			return nil, nil
		}
		identity.External = *email
	}
	if !old && idp != nil {
		if identity.Ref == nil {
			subject := samlNameID{Format: nameID11 + "unspecified", Value: identity.External}
			if scimEmailAddress(identity.External) {
				subject.Format = nameID11 + "emailAddress"
			}
			identity.Ref = &samlUserRef{idp.Metadata.Issuer, subject}
		} else {
			identity.Ref.Issuer = idp.Metadata.Issuer
		}
	}
	if identity.Ref == nil && email == nil {
		return nil, nil
	}
	return identity, nil
}

func scimIdentityValue(identity scimIdentity) any {
	if identity.Ref != nil {
		return samlSSOID(*identity.Ref)
	}
	return map[string]string{"scim_external_id": identity.External}
}

func scimTokenIdP(ctx context.Context, s scimUserStore, info tokenInfo) (*idpConfig, error) {
	if info.IdP == nil {
		return nil, nil
	}
	return s.getIdP(ctx, *info.IdP)
}

// Validation finds the actual current/previous mapping without migrating it.
func validateSCIMUser(ctx context.Context, s scimUserStore, p peerClient, idp *idpConfig, input scimUserInput, limit int, operation, requestID string) (validSCIMUser, error) {
	user, err := validateSCIMModel(input, idp, limit, operation)
	if err != nil || user.Ref == nil {
		return user, err
	}
	issuers := append([]string{user.Ref.Issuer}, idp.Extra.OldIssuers...)
	for _, issuer := range issuers {
		ref := samlUserRef{issuer, user.Ref.Subject}
		uid, err := s.samlUser(ctx, ref)
		if err != nil {
			return user, err
		}
		if uid == nil {
			continue
		}
		account, err := p.account(ctx, *uid, requestID, true)
		if err != nil {
			return user, err
		}
		if account != nil {
			user.Ref = &ref
			break
		}
	}
	return user, nil
}

func scimExternalAccount(ctx context.Context, s scimUserStore, p peerClient, team gocql.UUID, external, requestID string) (*brigAccount, error) {
	uid, err := s.scimExternal(ctx, team, external)
	if err != nil || uid == nil {
		return nil, err
	}
	account, err := p.account(ctx, *uid, requestID, true)
	if err != nil {
		return nil, err
	}
	if account == nil {
		return nil, s.deleteSCIMExternal(ctx, team, external)
	}
	return account, nil
}

func scimCheckExternal(ctx context.Context, s scimUserStore, p peerClient, team gocql.UUID, identity scimIdentity, allowed *gocql.UUID, requestID string) error {
	account, err := scimExternalAccount(ctx, s, p, team, identity.External, requestID)
	if err != nil {
		return err
	}
	conflict := account != nil && (allowed == nil || account.ID != *allowed)
	if identity.Ref != nil {
		uid, err := s.samlUser(ctx, *identity.Ref)
		if err != nil {
			return err
		}
		if uid != nil {
			account, err := p.account(ctx, *uid, requestID, true)
			if err != nil {
				return err
			}
			conflict = conflict || account != nil && (allowed == nil || account.ID != *allowed)
		}
	}
	if conflict {
		detail := "externalId is already taken"
		if allowed != nil {
			detail = "externalId already in use by another Wire user"
		}
		return &scimError{Status: 409, Detail: detail}
	}
	return nil
}

func scimPersistIdentity(ctx context.Context, s scimUserStore, team, uid gocql.UUID, identity scimIdentity) error {
	if err := s.insertSCIMExternal(ctx, team, identity.External, uid); err != nil {
		return err
	}
	if identity.Ref != nil {
		return s.insertSAMLUser(ctx, *identity.Ref, uid)
	}
	return nil
}

func scimUserDocument(uid gocql.UUID, user validSCIMUser) map[string]any {
	object := user.RichInfo.object()
	object["schemas"] = []string{scimUserSchema, richListURN, richMapURN}
	object["id"] = uid.String()
	object["userName"] = user.Handle
	object["externalId"] = user.External
	object["displayName"] = user.Name
	object["active"] = user.Active
	if user.Language != nil {
		object["preferredLanguage"] = *user.Language
	}
	if user.Role != nil {
		object["roles"] = []any{map[string]string{"value": *user.Role}}
	}
	if user.Email != nil {
		email := map[string]any{"value": user.Email.Value}
		if user.Email.Type != nil {
			email["type"] = *user.Email.Type
		}
		if user.Email.Primary != nil {
			email["primary"] = *user.Email.Primary
		}
		object["emails"] = []any{email}
	}
	return object
}

func scimUserWithMeta(c Config, uid gocql.UUID, user validSCIMUser, meta scimUserMeta) (map[string]any, error) {
	object := scimUserDocument(uid, user)
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(object); err != nil {
		return nil, err
	}
	digest := sha256.Sum256(bytes.TrimSuffix(encoded.Bytes(), []byte("\n")))
	base, err := url.Parse(c.SCIMBaseURI)
	if err != nil {
		return nil, err
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/Users/" + uid.String()
	base.RawPath = ""
	object["meta"] = map[string]any{"resourceType": "User", "created": meta.Created.UTC().Format("2006-01-02T15:04:05.000Z"), "lastModified": meta.Updated.UTC().Format("2006-01-02T15:04:05.000Z"), "version": fmt.Sprintf("W/\"%x\"", digest), "location": base.String()}
	return object, nil
}

func synthesizeSCIMUser(ctx context.Context, c Config, s scimUserStore, p peerClient, account brigAccount, identity scimIdentity, emailOverride *scimEmail, requestID string) (map[string]any, error) {
	if !validAccountStatus(account.Status) || account.Name == "" || account.Locale == "" {
		return nil, errors.New("invalid Brig SCIM account")
	}
	rich, err := p.scimRichInfo(ctx, account.ID, requestID)
	if err != nil {
		return nil, err
	}
	meta, err := s.scimMeta(ctx, account.ID)
	if err != nil {
		return nil, err
	}
	role := "member"
	if account.Team != nil {
		role, err = p.scimRole(ctx, account.ID, *account.Team, requestID)
		if err != nil {
			return nil, err
		}
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	missing := meta == nil
	if missing {
		meta = &scimUserMeta{Created: now, Updated: now}
	}
	if emailOverride != nil {
		meta.EmailType = emailOverride.Type
		meta.EmailPrimary = emailOverride.Primary
	}
	handle := account.ID.String()
	if account.Handle != nil {
		handle = *account.Handle
	} else if err = p.scimAccountField(ctx, account.ID, "handle", map[string]string{"handle": handle}, requestID); err != nil {
		return nil, err
	}
	email := account.UnvalidatedEmail
	if email == nil {
		email = account.Email
	}
	if email == nil {
		email = identity.Email
	}
	var entry *scimEmail
	if email != nil {
		entry = &scimEmail{*email, meta.EmailType, meta.EmailPrimary}
	}
	language := strings.Split(account.Locale, "-")[0]
	user := validSCIMUser{External: identity.External, Handle: handle, Name: account.Name, Email: entry, Ref: identity.Ref, RichInfo: rich.normalized(), Active: account.Status == "active" || account.Status == "ephemeral", Language: &language, Role: &role}
	stored, err := scimUserWithMeta(c, account.ID, user, *meta)
	if err != nil {
		return nil, err
	}
	if missing {
		if err = s.writeSCIMMeta(ctx, account.ID, *meta); err != nil {
			return nil, err
		}
	}
	if account.ManagedBy != "scim" {
		if err = p.scimSetManaged(ctx, account.ID, requestID); err != nil {
			return nil, err
		}
	}
	if !reflect.DeepEqual(rich, user.RichInfo) {
		if err = p.scimAccountField(ctx, account.ID, "rich-info", map[string]any{"rich_info": richAssoc(user.RichInfo)}, requestID); err != nil {
			return nil, err
		}
	}
	return stored, nil
}

func getSCIMUser(ctx context.Context, c Config, s scimUserStore, p peerClient, info tokenInfo, idp *idpConfig, uid gocql.UUID, requestID string) (map[string]any, error) {
	account, err := p.account(ctx, uid, requestID, true)
	if err != nil {
		return nil, err
	}
	if account == nil || account.Team == nil || *account.Team != info.Team {
		return nil, nil
	}
	identity, err := scimAccountIdentity(*account, idp, false)
	if err != nil {
		return nil, err
	}
	if identity == nil {
		return nil, nil
	}
	old, err := scimAccountIdentity(*account, nil, true)
	if err != nil {
		return nil, err
	}
	stored, err := synthesizeSCIMUser(ctx, c, s, p, *account, *identity, nil, requestID)
	if err != nil {
		return nil, err
	}
	if err = scimCheckExternal(ctx, s, p, info.Team, *identity, &uid, requestID); err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(old, identity) {
		if err = p.scimAccountField(ctx, uid, "sso-id", scimIdentityValue(*identity), requestID); err != nil {
			return nil, err
		}
	}
	if account.ManagedBy != "scim" {
		if err = p.scimSetManaged(ctx, uid, requestID); err != nil {
			return nil, err
		}
	}
	if old != nil && old.Ref != nil && !reflect.DeepEqual(old.Ref, identity.Ref) {
		if err = s.deleteSAMLUser(ctx, uid, *old.Ref); err != nil {
			return nil, err
		}
	}
	if err = scimPersistIdentity(ctx, s, info.Team, uid, *identity); err != nil {
		return nil, err
	}
	return stored, nil
}

func deleteSCIMUser(ctx context.Context, s scimUserStore, p peerClient, info tokenInfo, uid gocql.UUID, requestID string) error {
	account, err := p.account(ctx, uid, requestID, true)
	if err != nil {
		return err
	}
	if account == nil {
		return p.deleteAccount(ctx, uid, requestID)
	}
	if account.Team == nil || *account.Team != info.Team {
		return nil
	}
	idp, err := scimTokenIdP(ctx, s, info)
	if err != nil {
		return err
	}
	for _, old := range []bool{true, false} {
		identity, err := scimAccountIdentity(*account, idp, old)
		if err != nil {
			return err
		}
		if identity == nil {
			continue
		}
		if identity.Ref != nil {
			if err = s.deleteSAMLUser(ctx, uid, *identity.Ref); err != nil {
				return err
			}
		}
		if err = s.deleteSCIMExternal(ctx, info.Team, identity.External); err != nil {
			return err
		}
	}
	if err = s.deleteSCIMMeta(ctx, uid); err != nil {
		return err
	}
	return p.deleteAccount(ctx, uid, requestID)
}

func createSCIMUser(ctx context.Context, c Config, s scimUserStore, p peerClient, info tokenInfo, user validSCIMUser, requestID string) (map[string]any, error) {
	state, err := s.scimCreation(ctx, info.Team, user.External)
	if err != nil {
		return nil, err
	}
	if state != nil {
		if !state.Creating {
			status, err := p.scimAccountStatus(ctx, state.UID, requestID)
			if err != nil {
				return nil, err
			}
			if status != nil && *status != "deleted" {
				return nil, &scimError{Status: 409, Detail: "ExternalId is already taken: user with status " + *status + " exists"}
			}
		}
		if err = deleteSCIMUser(ctx, s, p, info, state.UID, requestID); err != nil {
			return nil, err
		}
	}
	identity := scimIdentity{External: user.External, Ref: user.Ref}
	if user.Email != nil {
		identity.Email = &user.Email.Value
	}
	if err = scimCheckExternal(ctx, s, p, info.Team, identity, nil, requestID); err != nil {
		return nil, err
	}
	available, err := p.scimHandleAvailable(ctx, user.Handle, requestID)
	if err != nil {
		return nil, err
	}
	if !available {
		return nil, &scimError{Status: 409, Detail: "userName is already taken"}
	}
	uid, err := gocql.RandomUUID()
	if err != nil {
		return nil, err
	}
	if err = s.setSCIMCreation(ctx, info.Team, user.External, uid, true); err != nil {
		return nil, err
	}
	if err = p.createSCIMAccount(ctx, uid, info.Team, user, requestID); err != nil {
		return nil, err
	}
	if err = p.scimAccountField(ctx, uid, "rich-info", map[string]any{"rich_info": richAssoc(user.RichInfo)}, requestID); err != nil {
		return nil, err
	}
	account, err := p.account(ctx, uid, requestID, true)
	if err != nil {
		return nil, err
	}
	if account == nil {
		return nil, &scimError{Status: 500, Detail: "Server error: user vanished"}
	}
	stored, err := synthesizeSCIMUser(ctx, c, s, p, *account, identity, user.Email, requestID)
	if err != nil {
		return nil, err
	}
	if err = scimPersistIdentity(ctx, s, info.Team, uid, identity); err != nil {
		return nil, err
	}
	if user.Email != nil {
		validate, err := p.samlEmailValidation(ctx, info.Team, requestID)
		if err != nil {
			return nil, err
		}
		if err = p.updateSAMLEmail(ctx, uid, user.Email.Value, requestID, validate); err != nil {
			return nil, err
		}
	}
	status, err := p.scimAccountStatus(ctx, uid, requestID)
	if err != nil {
		return nil, err
	}
	if status == nil {
		return nil, errors.New("missing Brig account status")
	}
	// Native creation uses the reloaded status, not the requested active flag.
	active := stored["active"].(bool)
	if *status == "active" && !active || *status == "suspended" && active {
		next := "suspended"
		if active {
			next = "active"
		}
		if err = p.scimAccountField(ctx, uid, "status", map[string]string{"status": next}, requestID); err != nil {
			return nil, err
		}
	}
	if err = s.setSCIMCreation(ctx, info.Team, user.External, uid, false); err != nil {
		return nil, err
	}
	return stored, nil
}

func serveSCIMUsers(w http.ResponseWriter, r *http.Request, c Config, s scimUserStore, p peerClient) bool {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 3 || len(parts) > 4 || parts[0] != "scim" || parts[1] != "v2" || parts[2] != "Users" {
		return false
	}
	info, err := scimAuthenticate(r.Context(), s, r)
	if err != nil {
		scimFailure(w, err)
		return true
	}
	var uid gocql.UUID
	if len(parts) == 4 {
		uid, err = gocql.ParseUUID(parts[3])
		if err != nil {
			scimFailure(w, &scimError{Status: 400, Detail: "Invalid user ID"})
			return true
		}
	}
	ctx := r.Context()
	requestID := r.Header.Get("Request-Id")
	var body map[string]any
	status := 200
	switch r.Method {
	case "POST", "PUT":
		if r.Method == "POST" && len(parts) != 3 || r.Method == "PUT" && len(parts) != 4 {
			w.WriteHeader(405)
			return true
		}
		var object map[string]any
		object, err = scimRawJSON(r)
		if err == nil {
			var input scimUserInput
			input, err = parseSCIMUser(object)
			if err == nil {
				var idp *idpConfig
				idp, err = scimTokenIdP(ctx, s, *info)
				if err == nil {
					var user validSCIMUser
					operation := "post"
					if r.Method == "PUT" {
						operation = "put"
					}
					user, err = validateSCIMUser(ctx, s, p, idp, input, *c.RichInfoLimit, operation, requestID)
					if err == nil {
						if r.Method == "POST" {
							body, err = createSCIMUser(ctx, c, s, p, *info, user, requestID)
							status = 201
						} else {
							body, err = updateSCIMUser(ctx, c, s, p, *info, idp, uid, user, requestID)
						}
					}
				}
			}
		}
	case "GET", "HEAD":
		var idp *idpConfig
		idp, err = scimTokenIdP(ctx, s, *info)
		if err == nil {
			if len(parts) == 3 {
				body, err = listSCIMUsers(ctx, c, s, p, *info, idp, r.URL.Query(), requestID)
			} else {
				body, err = getSCIMUser(ctx, c, s, p, *info, idp, uid, requestID)
			}
		}
		if err == nil && body == nil {
			err = scimMissingUser(uid)
		}
	case "PATCH":
		if len(parts) != 4 {
			w.WriteHeader(405)
			return true
		}
		var object map[string]any
		object, err = scimRawJSON(r)
		if err == nil {
			var operations []scimPatchOperation
			operations, err = parseSCIMPatch(object)
			if err == nil {
				var idp *idpConfig
				idp, err = scimTokenIdP(ctx, s, *info)
				if err == nil {
					var stored map[string]any
					stored, err = getSCIMUser(ctx, c, s, p, *info, idp, uid, requestID)
					if err == nil && stored == nil {
						err = scimMissingUser(uid)
					}
					if err == nil {
						var input scimUserInput
						input, err = scimStoredInput(stored)
						if err == nil {
							input, err = applySCIMPatch(input, operations)
						}
						if err == nil {
							var user validSCIMUser
							user, err = validateSCIMUser(ctx, s, p, idp, input, *c.RichInfoLimit, "put", requestID)
							if err == nil {
								body, err = updateSCIMUser(ctx, c, s, p, *info, idp, uid, user, requestID)
							}
						}
					}
				}
			}
		}
	case "DELETE":
		if len(parts) != 4 {
			w.WriteHeader(405)
			return true
		}
		err = deleteSCIMUser(ctx, s, p, *info, uid, requestID)
		if err == nil {
			w.WriteHeader(204)
			return true
		}
	default:
		w.WriteHeader(405)
		return true
	}
	if err != nil {
		scimFailure(w, err)
	} else {
		scimResponse(w, r, status, body)
	}
	return true
}
