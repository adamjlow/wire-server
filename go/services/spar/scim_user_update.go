// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"reflect"
	"strings"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func scimStoredInput(stored map[string]any) (scimUserInput, error) {
	encoded, err := json.Marshal(stored)
	if err != nil {
		return scimUserInput{}, err
	}
	d := json.NewDecoder(bytes.NewReader(encoded))
	d.UseNumber()
	var object map[string]any
	if err = d.Decode(&object); err != nil {
		return scimUserInput{}, err
	}
	return parseSCIMUser(object)
}
func scimUserIdentity(user validSCIMUser) scimIdentity {
	identity := scimIdentity{External: user.External, Ref: user.Ref}
	if user.Email != nil {
		identity.Email = &user.Email.Value
	}
	return identity
}
func scimMissingUser(uid gocql.UUID) error {
	return &scimError{Status: 404, Detail: "User " + uid.String() + " not found"}
}
func scimStatusTransition(status string, active bool) string {
	if status == "active" && !active {
		return "suspended"
	}
	if status == "suspended" && active {
		return "active"
	}
	return status
}
func (p peerClient) scimDefaultLocale(ctx context.Context, requestID string) (string, error) {
	status, body, err := p.request(ctx, p.config.Brig, "GET", "/i/users/locale", requestID, "", nil)
	if err != nil {
		return "", err
	}
	var result struct {
		Locale *string `json:"locale"`
	}
	if status != 200 || json.Unmarshal(body, &result) != nil || result.Locale == nil {
		return "", errors.New("invalid Brig default locale response")
	}
	return *result.Locale, nil
}
func (p peerClient) scimUpdateRole(ctx context.Context, uid, team gocql.UUID, role, requestID string) error {
	bits := map[string]uint64{"owner": 8191, "admin": 5951, "member": 1587, "partner": 1025}[role]
	// Native internal member update does not interpret the response body/status.
	_, _, err := p.request(ctx, p.config.Galley, "PUT", "/i/teams/"+team.String()+"/members", requestID, "", map[string]any{"user": uid.String(), "permissions": map[string]uint64{"self": bits, "copy": bits}})
	return err
}
func validateSCIMEmail(ctx context.Context, p peerClient, info tokenInfo, uid gocql.UUID, email *string, requestID string) error {
	if email == nil {
		return nil
	}
	validate, err := p.samlEmailValidation(ctx, info.Team, requestID)
	if err != nil {
		return err
	}
	return p.updateSAMLEmail(ctx, uid, *email, requestID, validate)
}
func updateSCIMUser(ctx context.Context, c Config, s scimUserStore, p peerClient, info tokenInfo, idp *idpConfig, uid gocql.UUID, next validSCIMUser, requestID string) (map[string]any, error) {
	stored, err := getSCIMUser(ctx, c, s, p, info, idp, uid, requestID)
	if err != nil {
		return nil, err
	}
	if stored == nil {
		return nil, scimMissingUser(uid)
	}
	input, err := scimStoredInput(stored)
	if err != nil {
		return nil, err
	}
	old, err := validateSCIMUser(ctx, s, p, idp, input, *c.RichInfoLimit, "recover-old-value", requestID)
	if err != nil {
		return nil, err
	}
	locale, err := p.scimDefaultLocale(ctx, requestID)
	if err != nil {
		return nil, err
	}
	if next.Language == nil {
		next.Language = &locale
	}
	nextIdentity := scimUserIdentity(next)
	oldIdentity := scimUserIdentity(old)
	if err = scimCheckExternal(ctx, s, p, info.Team, nextIdentity, &uid, requestID); err != nil {
		return nil, err
	}
	account, err := p.account(ctx, uid, requestID, true)
	if err != nil {
		return nil, err
	}
	if account == nil || account.Handle == nil || *account.Handle != next.Handle {
		available, err := p.scimHandleAvailable(ctx, next.Handle, requestID)
		if err != nil {
			return nil, err
		}
		if !available {
			return nil, &scimError{Status: 409, Detail: "userName already in use by another wire user"}
		}
	}
	if reflect.DeepEqual(old, next) {
		return stored, nil
	}
	meta, err := s.scimMeta(ctx, uid)
	if err != nil {
		return nil, err
	}
	if meta == nil {
		return nil, errors.New("missing SCIM user metadata")
	}
	meta.Updated = time.Now().UTC().Truncate(time.Millisecond)
	meta.EmailType = nil
	meta.EmailPrimary = nil
	if next.Email != nil {
		meta.EmailType = next.Email.Type
		meta.EmailPrimary = next.Email.Primary
	}
	// Native ValidScimId equality includes the authentication email.
	if !reflect.DeepEqual(oldIdentity, nextIdentity) {
		if !reflect.DeepEqual(oldIdentity.Email, nextIdentity.Email) {
			if err = validateSCIMEmail(ctx, p, info, uid, nextIdentity.Email, requestID); err != nil {
				return nil, err
			}
		}
		if err = s.deleteSCIMExternal(ctx, info.Team, old.External); err != nil {
			return nil, err
		}
		if old.Ref != nil {
			if err = s.deleteSAMLUser(ctx, uid, *old.Ref); err != nil {
				return nil, err
			}
		}
		if err = scimPersistIdentity(ctx, s, info.Team, uid, nextIdentity); err != nil {
			return nil, err
		}
		if err = p.scimAccountField(ctx, uid, "sso-id", scimIdentityValue(nextIdentity), requestID); err != nil {
			return nil, err
		}
	}
	if next.Name != old.Name {
		if err = p.scimAccountField(ctx, uid, "name", map[string]string{"name": next.Name}, requestID); err != nil {
			return nil, err
		}
	}
	if next.Handle != old.Handle {
		if err = p.scimAccountField(ctx, uid, "handle", map[string]string{"handle": next.Handle}, requestID); err != nil {
			return nil, err
		}
	}
	if !reflect.DeepEqual(next.RichInfo, old.RichInfo) {
		if err = p.scimAccountField(ctx, uid, "rich-info", map[string]any{"rich_info": richAssoc(next.RichInfo)}, requestID); err != nil {
			return nil, err
		}
	}
	if !reflect.DeepEqual(next.Language, old.Language) {
		if err = p.scimAccountField(ctx, uid, "locale", map[string]string{"locale": *next.Language}, requestID); err != nil {
			return nil, err
		}
	}
	if next.Role != nil && !reflect.DeepEqual(next.Role, old.Role) {
		if err = p.scimUpdateRole(ctx, uid, info.Team, *next.Role, requestID); err != nil {
			return nil, err
		}
	}
	status, err := p.scimAccountStatus(ctx, uid, requestID)
	if err != nil {
		return nil, err
	}
	if status != nil {
		if newStatus := scimStatusTransition(*status, next.Active); newStatus != *status {
			if err = p.scimAccountField(ctx, uid, "status", map[string]string{"status": newStatus}, requestID); err != nil {
				return nil, err
			}
		}
	}
	if err = s.writeSCIMMeta(ctx, uid, *meta); err != nil {
		return nil, err
	}
	return getSCIMUser(ctx, c, s, p, info, idp, uid, requestID)
}

func listSCIMUsers(ctx context.Context, c Config, s scimUserStore, p peerClient, info tokenInfo, idp *idpConfig, query url.Values, requestID string) (map[string]any, error) {
	values, exists := query["filter"]
	if !exists || len(values) == 0 {
		return nil, &scimError{Status: 400, Type: "tooMany", Detail: "Please specify a filter when getting users."}
	}
	filter, err := scimParseComparison(scimTrimSpace(values[0]), []string{scimUserSchema})
	if err != nil {
		return nil, &scimError{Status: 400, Type: "invalidFilter", Detail: "Invalid filter"}
	}
	value, ok := filter.Value.(string)
	if filter.Op != "eq" || !ok {
		return nil, &scimError{Status: 400, Type: "invalidFilter", Detail: "Operation not supported"}
	}
	var uid *gocql.UUID
	switch strings.ToLower(filter.Attr.Name) {
	case "username":
		handle := strings.ToLower(value)
		if validWireHandle(handle) {
			account, err := p.scimAccountSearch(ctx, "handles", handle, requestID)
			if err != nil {
				return nil, err
			}
			if account != nil {
				uid = &account.ID
			}
		}
	case "externalid":
		uid, err = s.scimExternal(ctx, info.Team, value)
		if err != nil {
			return nil, err
		}
		if uid == nil {
			// The native lookup validates an identity, not a display name.
			name := "lookup"
			input := scimUserInput{UserName: "lookup", External: &value, DisplayName: &name}
			user, err := validateSCIMUser(ctx, s, p, idp, input, *c.RichInfoLimit, "lookup", requestID)
			if err != nil {
				var failure *scimError
				if !errors.As(err, &failure) {
					return nil, err
				}
			} else {
				var viaEmail, viaRef *gocql.UUID
				if user.Email != nil {
					account, err := p.scimAccountSearch(ctx, "email", user.Email.Value, requestID)
					if err != nil {
						return nil, err
					}
					if account != nil {
						viaEmail = &account.ID
					}
				}
				if user.Ref != nil {
					viaRef, err = s.samlUser(ctx, *user.Ref)
					if err != nil {
						return nil, err
					}
				}
				uid = viaEmail
				if uid == nil {
					uid = viaRef
				}
			}
			if uid != nil {
				account, err := p.account(ctx, *uid, requestID, true)
				if err != nil {
					return nil, err
				}
				if account == nil {
					uid = nil
				} else {
					uid = &account.ID
				}
			}
		}
	default:
		return nil, &scimError{Status: 400, Type: "invalidFilter", Detail: "Unsupported attribute"}
	}
	resources := []any{}
	if uid != nil {
		stored, err := getSCIMUser(ctx, c, s, p, info, idp, *uid, requestID)
		if err != nil {
			return nil, err
		}
		if stored != nil {
			resources = append(resources, stored)
		}
	}
	return map[string]any{"schemas": []string{scimListSchema}, "totalResults": len(resources), "itemsPerPage": len(resources), "startIndex": 1, "Resources": resources}, nil
}
