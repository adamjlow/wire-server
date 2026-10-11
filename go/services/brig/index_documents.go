// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"net/url"
	"time"
	"unicode/utf8"
)

type indexRole struct {
	name    string
	written time.Time
}
type indexSSO struct {
	Issuer string `json:"issuer"`
	NameID string `json:"nameid"`
}

// Native UserDoc writes every field, including nulls, so an upsert removes
// previously indexed values when a user becomes a tombstone.
type indexDocument struct {
	ID               string    `json:"id"`
	Type             *string   `json:"type"`
	Team             *string   `json:"team"`
	Name             *string   `json:"name"`
	Normalized       *string   `json:"normalized"`
	Handle           *string   `json:"handle"`
	Email            *string   `json:"email"`
	Accent           *int32    `json:"accent_id"`
	Status           *string   `json:"account_status"`
	SAMLIDP          *string   `json:"saml_idp"`
	ManagedBy        *string   `json:"managed_by"`
	Created          *string   `json:"created_at"`
	Role             *string   `json:"role"`
	Visibility       *string   `json:"search_visibility_inbound"`
	SCIMExternal     *string   `json:"scim_external_id"`
	SSO              *indexSSO `json:"sso"`
	EmailUnvalidated *string   `json:"email_unvalidated"`
	Searchable       *bool     `json:"searchable"`
}

func (indexDocument) String() string { return "<IndexDocument>" }

func indexIdentity(raw json.RawMessage, managed *int32) (*indexSSO, *string, error) {
	if len(raw) == 0 {
		return nil, nil, nil
	}
	var identity struct {
		Tenant, Subject *string
		External        *string `json:"scim_external_id"`
	}
	if json.Unmarshal(raw, &identity) != nil {
		return nil, nil, errors.New("invalid index SSO identity")
	}
	if identity.Tenant == nil && identity.Subject == nil && identity.External != nil {
		if managed == nil {
			return nil, nil, nil
		}
		return nil, identity.External, nil
	}
	if identity.Tenant == nil || identity.Subject == nil || identity.External != nil {
		return nil, nil, errors.New("invalid index SSO identity")
	}
	// Native storage accepts both XML and historical plain issuer/name forms.
	elementText := func(raw, local string) (string, bool) {
		var elem struct {
			XMLName xml.Name
			Text    string `xml:",chardata"`
		}
		err := xml.Unmarshal([]byte(raw), &elem)
		return elem.Text, err == nil && elem.XMLName.Local == local && elem.XMLName.Space == "urn:oasis:names:tc:SAML:2.0:assertion"
	}
	issuer, subject := *identity.Tenant, *identity.Subject
	if text, ok := elementText(issuer, "Issuer"); ok {
		issuer = text
	}
	uri, err := url.Parse(issuer)
	if err != nil || uri.Scheme == "" {
		return nil, nil, errors.New("invalid index SSO issuer")
	}
	if text, ok := elementText(subject, "NameID"); ok {
		subject = text
	} else if validateIndexSubject(subject) != nil {
		return nil, nil, errors.New("invalid index SSO subject")
	}
	sso := &indexSSO{Issuer: uri.String(), NameID: subject}
	var external *string
	if managed != nil && *managed == 1 {
		external = &sso.NameID
	}
	return sso, external, nil
}

func validateIndexSubject(subject string) error {
	// Native plain Name is 1..128 Unicode characters; XML NameID may carry
	// longer historical identifiers and is validated by the XML parser above.
	length := utf8.RuneCountInString(subject)
	if !utf8.ValidString(subject) || length < 1 || length > 128 {
		return errors.New("invalid index SSO subject")
	}
	return nil
}

func (r indexUser) document(visibility string, role *indexRole) (indexDocument, error) {
	u := r.user
	d := indexDocument{ID: u.ID.String()}
	if u.Status != nil && (*u.Status < 0 || int(*u.Status) >= len(accountStatusNames)) {
		return d, errors.New("invalid index account status")
	}
	if u.Type != nil && (*u.Type < 0 || *u.Type > 2) {
		return d, errors.New("invalid index user type")
	}
	if u.ManagedBy != nil && (*u.ManagedBy < 0 || *u.ManagedBy > 1) {
		return d, errors.New("invalid index managed-by")
	}
	if !u.Activated || u.Service != nil || u.Status != nil && *u.Status >= 2 {
		return d, nil
	}
	if visibility != "searchable-by-own-team" && visibility != "searchable-by-all-teams" {
		return d, errors.New("invalid index visibility")
	}
	normalized, err := normalizedSearchName(u.Name)
	if err != nil {
		return d, err
	}
	sso, external, err := indexIdentity(u.SSO, u.ManagedBy)
	if err != nil {
		return d, err
	}
	typ := "regular"
	if u.Type != nil {
		typ = []string{"regular", "bot", "app"}[*u.Type]
	}
	d.Type, d.Name, d.Normalized, d.Handle, d.Email, d.Accent = &typ, &u.Name, &normalized, u.Handle, u.Email, &u.Accent
	d.EmailUnvalidated, d.Searchable, d.Visibility, d.SSO, d.SCIMExternal = u.EmailUnvalidated, u.Searchable, &visibility, sso, external
	if sso != nil {
		d.SAMLIDP = &sso.Issuer
	}
	if u.Team != nil {
		team := u.Team.String()
		d.Team = &team
	}
	if u.Status != nil {
		status := accountStatusNames[*u.Status]
		d.Status = &status
	}
	if u.ManagedBy != nil {
		managed := []string{"wire", "scim"}[*u.ManagedBy]
		d.ManagedBy = &managed
	}
	created := r.created.UTC().Format("2006-01-02T15:04:05.000Z")
	d.Created = &created
	if role != nil {
		d.Role = &role.name
	}
	return d, nil
}
