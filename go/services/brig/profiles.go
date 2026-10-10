// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"encoding/json"
	"errors"
)

type qualifiedUserID struct {
	ID     string `json:"id"`
	Domain string `json:"domain"`
}
type serviceReference struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
}
type profileAsset struct {
	Type string  `json:"type"`
	Key  string  `json:"key"`
	Size *string `json:"size,omitempty"`
}

// This is the native User/SelfProfile wire object. Storage defaults and
// visibility are applied before encoding; optional identity fields are absent.
type selfProfile struct {
	QualifiedID      qualifiedUserID   `json:"qualified_id"`
	ID               string            `json:"id"`
	Type             string            `json:"type"`
	Email            *string           `json:"email,omitempty"`
	SSO              json.RawMessage   `json:"sso_id,omitempty"`
	EmailUnvalidated *string           `json:"email_unvalidated,omitempty"`
	Name             string            `json:"name"`
	TextStatus       *string           `json:"text_status,omitempty"`
	Picture          []json.RawMessage `json:"picture"`
	Assets           []profileAsset    `json:"assets"`
	Accent           int32             `json:"accent_id"`
	Status           string            `json:"status"`
	Locale           string            `json:"locale"`
	Service          *serviceReference `json:"service,omitempty"`
	Handle           *string           `json:"handle,omitempty"`
	Expires          *string           `json:"expires_at,omitempty"`
	Team             *string           `json:"team,omitempty"`
	ManagedBy        string            `json:"managed_by"`
	Protocols        []string          `json:"supported_protocols"`
	Deleted          bool              `json:"deleted,omitempty"`
	Searchable       bool              `json:"searchable"`
}

var accountStatusNames = []string{"active", "suspended", "deleted", "ephemeral", "pending-invitation"}

func (user storedUser) selfProfile(domain, defaultLocale string) (selfProfile, error) {
	profile := selfProfile{QualifiedID: qualifiedUserID{user.ID.String(), domain}, ID: user.ID.String(), Type: "regular", Name: user.Name, TextStatus: user.TextStatus, EmailUnvalidated: user.EmailUnvalidated, Picture: user.Picture, Assets: []profileAsset{}, Accent: user.Accent, Status: "active", Locale: defaultLocale, Handle: user.Handle, ManagedBy: "wire", Protocols: []string{"proteus"}, Searchable: true}
	if profile.Picture == nil {
		profile.Picture = []json.RawMessage{}
	}
	if profile.Locale == "" {
		profile.Locale = "en"
	}
	if user.Type != nil {
		switch *user.Type {
		case 0:
			profile.Type = "regular"
		case 1:
			profile.Type = "bot"
		case 2:
			profile.Type = "app"
		default:
			return profile, errors.New("invalid stored user type")
		}
	} else if user.Provider != nil && user.Service != nil {
		profile.Type = "bot"
	}
	if user.Status != nil {
		if *user.Status < 0 || int(*user.Status) >= len(accountStatusNames) {
			return profile, errors.New("invalid stored account status")
		}
		profile.Status = accountStatusNames[*user.Status]
		profile.Deleted = *user.Status == 2
		if *user.Status == 3 && user.Expires != nil {
			x := user.Expires.UTC().Format("2006-01-02T15:04:05.000Z")
			profile.Expires = &x
		}
	}
	if user.Activated {
		profile.Email = user.Email
		profile.SSO = user.SSO
	}
	if user.Language != nil {
		profile.Locale = *user.Language
		if user.Country != nil {
			profile.Locale += "-" + *user.Country
		}
	}
	if user.Provider != nil && user.Service != nil {
		profile.Service = &serviceReference{user.Service.String(), user.Provider.String()}
	}
	if user.Team != nil {
		x := user.Team.String()
		profile.Team = &x
	}
	if user.ManagedBy != nil {
		switch *user.ManagedBy {
		case 0:
			profile.ManagedBy = "wire"
		case 1:
			profile.ManagedBy = "scim"
		default:
			return profile, errors.New("invalid stored management mode")
		}
	}
	if user.Protocols != nil && *user.Protocols&3 != 0 {
		profile.Protocols = []string{}
		if *user.Protocols&1 != 0 {
			profile.Protocols = append(profile.Protocols, "proteus")
		}
		if *user.Protocols&2 != 0 {
			profile.Protocols = append(profile.Protocols, "mls")
		}
	}
	if user.Searchable != nil {
		profile.Searchable = *user.Searchable
	}
	for _, asset := range user.Assets {
		projected := profileAsset{Type: "image", Key: asset.Key}
		if asset.Size != nil {
			var size string
			switch *asset.Size {
			case 0:
				size = "preview"
			case 1:
				size = "complete"
			default:
				return profile, errors.New("invalid stored asset size")
			}
			projected.Size = &size
		}
		profile.Assets = append(profile.Assets, projected)
	}
	return profile, nil
}
