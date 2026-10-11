// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"encoding/json"
	"errors"
	"math"
	"time"
	"unicode/utf8"
)

func decodeRemoteUserProfiles(raw json.RawMessage) ([]publicUserProfile, error) {
	invalid := errors.New("invalid remote user profiles")
	if !validRegistrationJSONText(raw) {
		return nil, invalid
	}
	var values []json.RawMessage
	if string(raw) == "null" || json.Unmarshal(raw, &values) != nil {
		return nil, invalid
	}
	result := make([]publicUserProfile, 0, len(values))
	for _, value := range values {
		fields, err := registrationObject(value)
		if err != nil {
			return nil, invalid
		}
		for _, key := range []string{"qualified_id", "name", "accent_id", "legalhold_status"} {
			if !registrationPresent(fields[key]) {
				return nil, invalid
			}
		}
		// Native legacy picture/assets parsers fall back to an empty list.
		assets, _ := registrationAssets(fields["assets"])
		delete(fields, "assets")
		delete(fields, "picture")
		accentRaw := fields["accent_id"]
		p := publicUserProfile{Type: "regular", Searchable: true, Protocols: []string{"proteus"}, Picture: []json.RawMessage{}, Assets: []profileAsset{}}
		delete(fields, "accent_id")
		withoutAccent, _ := json.Marshal(fields)
		if json.Unmarshal(withoutAccent, &p) != nil {
			return nil, invalid
		}
		n, err := federationInteger(accentRaw)
		if err != nil || n < math.MinInt32 || n > math.MaxInt32 {
			return nil, invalid
		}
		p.Accent = int32(n)
		id, ok := tokenUUID(p.QualifiedID.ID)
		if !ok {
			return nil, invalid
		}
		domain, err := parseEmailDomain(p.QualifiedID.Domain)
		if err != nil {
			return nil, invalid
		}
		p.QualifiedID = qualifiedUserID{id, domain}
		if registrationPresent(fields["id"]) {
			if _, ok := tokenUUID(p.ID); !ok {
				return nil, invalid
			}
		}
		count := utf8.RuneCountInString(p.Name)
		if count < 1 || count > 128 {
			return nil, invalid
		}
		if p.TextStatus != nil {
			count = utf8.RuneCountInString(*p.TextStatus)
			if count < 1 || count > 256 {
				return nil, invalid
			}
		}
		if p.Team != nil {
			canonical, ok := tokenUUID(*p.Team)
			if !ok {
				return nil, invalid
			}
			p.Team = &canonical
		}
		if p.Service != nil {
			if _, ok := tokenUUID(p.Service.ID); !ok {
				return nil, invalid
			}
			if _, ok := tokenUUID(p.Service.Provider); !ok {
				return nil, invalid
			}
		}
		if p.Handle != nil && !validLoginHandle(*p.Handle) {
			return nil, invalid
		}
		if p.Email != nil {
			if _, err := canonicalRegistrationEmail(*p.Email); err != nil {
				return nil, invalid
			}
		}
		if !registrationPresent(fields["supported_protocols"]) {
			p.Protocols = []string{"proteus"}
		}
		if p.App != nil {
			app, err := registrationObject(fields["app"])
			if err != nil {
				return nil, invalid
			}
			if _, err = registrationText(app, "category", true); err != nil {
				return nil, invalid
			}
			if _, err = registrationText(app, "description", true); err != nil {
				return nil, invalid
			}
		}
		if p.Expires != nil {
			if _, err := time.Parse(time.RFC3339Nano, *p.Expires); err != nil {
				return nil, invalid
			}
		}
		switch p.Type {
		case "regular", "app", "bot":
		default:
			return nil, invalid
		}
		switch p.Legalhold {
		case "no_consent", "disabled", "pending", "enabled":
		default:
			return nil, invalid
		}
		for _, protocol := range p.Protocols {
			if protocol != "proteus" && protocol != "mls" {
				return nil, invalid
			}
		}
		if p.App != nil && utf8.RuneCountInString(p.App.Description) > 300 {
			return nil, invalid
		}
		if p.Contact != nil && p.Contact.State != "contactable" && p.Contact.State != "non-contactable" {
			return nil, invalid
		}
		for _, asset := range assets {
			a := profileAsset{Type: "image", Key: asset.Key}
			if asset.Size != nil {
				size := "preview"
				if *asset.Size == 1 {
					size = "complete"
				}
				a.Size = &size
			}
			p.Assets = append(p.Assets, a)
		}
		result = append(result, p)
	}
	return result, nil
}
