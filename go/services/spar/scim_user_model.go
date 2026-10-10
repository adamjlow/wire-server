// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"sort"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/cases"
)

const scimUserSchema = "urn:ietf:params:scim:schemas:core:2.0:User"
const richMapURN = "urn:ietf:params:scim:schemas:extension:wire:1.0:User"
const richListURN = "urn:wire:scim:schemas:profile:1.0"

type richField struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}
type richInfo []richField

func (r richInfo) normalized() richInfo {
	out := richInfo{}
	seen := map[string]bool{}
	for _, field := range r {
		key := strings.ToLower(cases.Fold().String(field.Type))
		if field.Value != "" && !seen[key] {
			out = append(out, field)
			seen[key] = true
		}
	}
	return out
}
func (r richInfo) size() int {
	n := 0
	for _, field := range r {
		n += utf8.RuneCountInString(field.Type) + utf8.RuneCountInString(field.Value)
	}
	return n
}
func (r richInfo) object() map[string]any {
	r = r.normalized()
	values := map[string]string{}
	for _, field := range r {
		values[field.Type] = field.Value
	}
	return map[string]any{richMapURN: values, richListURN: map[string]any{"richInfo": map[string]any{"version": 0, "fields": r}}}
}
func scimFoldFields(object map[string]any) map[string]any {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := map[string]any{}
	for _, key := range keys {
		out[cases.Fold().String(key)] = object[key]
	}
	return out
}
func parseRichFields(value any, versioned bool) (richInfo, error) {
	bad := &scimError{Status: 400, Type: "invalidSyntax", Detail: "Invalid rich info"}
	if versioned {
		obj, ok := value.(map[string]any)
		if !ok {
			return nil, bad
		}
		obj = scimFoldFields(obj)
		n, ok := obj["version"].(json.Number)
		if !ok {
			return nil, bad
		}
		mantissa := strings.SplitN(strings.ToLower(string(n)), "e", 2)[0]
		if strings.Trim(mantissa, "-0.") != "" {
			return nil, bad
		}
		value = obj["fields"]
	}
	values, ok := value.([]any)
	if !ok {
		return nil, bad
	}
	out := richInfo{}
	seen := map[string]bool{}
	for _, value := range values {
		obj, ok := value.(map[string]any)
		if !ok {
			return nil, bad
		}
		obj = scimFoldFields(obj)
		key, ok := obj["type"].(string)
		if !ok {
			return nil, bad
		}
		text, ok := obj["value"].(string)
		if !ok {
			return nil, bad
		}
		folded := cases.Fold().String(key)
		if versioned && seen[folded] {
			return nil, bad
		}
		seen[folded] = true
		out = append(out, richField{key, text})
	}
	return out, nil
}
func parseRichInfo(input map[string]any) (richInfo, error) {
	input = scimFoldFields(input)
	out := richInfo{}
	if value, exists := input[cases.Fold().String(richListURN)]; exists {
		obj, ok := value.(map[string]any)
		if !ok {
			return nil, &scimError{Status: 400, Type: "invalidSyntax", Detail: "Invalid rich info"}
		}
		value, exists = scimFoldFields(obj)["richinfo"]
		if !exists {
			return nil, &scimError{Status: 400, Type: "invalidSyntax", Detail: "Missing richInfo"}
		}
		_, versioned := value.(map[string]any)
		var err error
		out, err = parseRichFields(value, versioned)
		if err != nil {
			return nil, err
		}
	}
	if value, exists := input[cases.Fold().String(richMapURN)]; exists {
		obj, ok := value.(map[string]any)
		if !ok {
			return nil, &scimError{Status: 400, Type: "invalidSyntax", Detail: "Invalid rich info map"}
		}
		keys := make([]string, 0, len(obj))
		for key := range obj {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(i, j int) bool { return cases.Fold().String(keys[i]) < cases.Fold().String(keys[j]) })
		for _, key := range keys {
			text, ok := obj[key].(string)
			if !ok {
				return nil, &scimError{Status: 400, Type: "invalidSyntax", Detail: "Invalid rich info value"}
			}
			folded := cases.Fold().String(key)
			replaced := false
			for i := range out {
				if cases.Fold().String(out[i].Type) == folded {
					out[i] = richField{key, text}
					replaced = true
					break
				}
			}
			if !replaced {
				out = append(out, richField{key, text})
			}
		}
	}
	return out.normalized(), nil
}

type scimEmail struct {
	Value   string
	Type    *string
	Primary *bool
}

func scimOptionalString(obj map[string]any, key string) (*string, error) {
	v, ok := obj[key]
	if !ok || v == nil {
		return nil, nil
	}
	s, ok := v.(string)
	if !ok {
		return nil, &scimError{Status: 400, Type: "invalidSyntax", Detail: "Invalid " + key}
	}
	return &s, nil
}
func scimOptionalBool(obj map[string]any, key string) (*bool, error) {
	v, ok := obj[key]
	if !ok || v == nil {
		return nil, nil
	}
	var b bool
	switch v := v.(type) {
	case bool:
		b = v
	case string:
		if strings.EqualFold(v, "true") {
			b = true
		} else if !strings.EqualFold(v, "false") {
			return nil, &scimError{Status: 400, Type: "invalidSyntax", Detail: "Invalid " + key}
		}
	default:
		return nil, &scimError{Status: 400, Type: "invalidSyntax", Detail: "Invalid " + key}
	}
	return &b, nil
}
func scimEmailAddress(value string) bool {
	address, err := mail.ParseAddress(value)
	return err == nil && address.Name == "" && address.Address == value
}
func parseSCIMEmails(obj map[string]any) ([]scimEmail, error) {
	raw, exists := obj["emails"]
	if !exists || raw == nil {
		return []scimEmail{}, nil
	}
	values, ok := raw.([]any)
	if !ok {
		return nil, &scimError{Status: 400, Type: "invalidSyntax", Detail: "Invalid emails"}
	}
	out := []scimEmail{}
	for _, value := range values {
		entry, ok := value.(map[string]any)
		if !ok {
			return nil, &scimError{Status: 400, Type: "invalidSyntax", Detail: "Invalid email"}
		}
		entry = scimFoldFields(entry)
		email, ok := entry["value"].(string)
		if !ok || !scimEmailAddress(email) {
			return nil, &scimError{Status: 400, Type: "invalidSyntax", Detail: "Invalid email"}
		}
		typ, err := scimOptionalString(entry, "type")
		if err != nil {
			return nil, err
		}
		primary, err := scimOptionalBool(entry, "primary")
		if err != nil {
			return nil, err
		}
		out = append(out, scimEmail{email, typ, primary})
	}
	return out, nil
}
func selectSCIMEmail(emails []scimEmail) (*scimEmail, error) {
	var selected *scimEmail
	for i := range emails {
		if emails[i].Primary != nil && *emails[i].Primary {
			if selected != nil {
				return nil, &scimError{Status: 400, Type: "invalidValue", Detail: "More than one email is marked as primary; RFC 7643 §2.4 allows at most one."}
			}
			selected = &emails[i]
		}
	}
	if selected == nil && len(emails) > 0 {
		selected = &emails[0]
	}
	return selected, nil
}

type scimUserInput struct {
	UserName                                  string
	External, DisplayName, Language, Password *string
	Active                                    *bool
	Emails                                    []scimEmail
	Roles                                     []*string
	RichInfo                                  richInfo
}

func parseSCIMUser(object map[string]any) (scimUserInput, error) {
	var user scimUserInput
	rich, err := parseRichInfo(object)
	if err != nil {
		return user, err
	}
	user.RichInfo = rich
	object = scimFoldFields(object)
	name, ok := object["username"].(string)
	if !ok {
		return user, &scimError{Status: 400, Type: "invalidSyntax", Detail: "userName is required"}
	}
	user.UserName = name
	for _, field := range []struct {
		key    string
		target **string
	}{{"externalid", &user.External}, {"displayname", &user.DisplayName}, {"preferredlanguage", &user.Language}, {"password", &user.Password}} {
		*field.target, err = scimOptionalString(object, field.key)
		if err != nil {
			return user, err
		}
	}
	user.Active, err = scimOptionalBool(object, "active")
	if err != nil {
		return user, err
	}
	user.Emails, err = parseSCIMEmails(object)
	if err != nil {
		return user, err
	}
	if value, exists := object["roles"]; exists && value != nil {
		roles, ok := value.([]any)
		if !ok {
			return user, &scimError{Status: 400, Type: "invalidSyntax", Detail: "Invalid roles"}
		}
		for _, value := range roles {
			role, ok := value.(map[string]any)
			if !ok {
				return user, &scimError{Status: 400, Type: "invalidSyntax", Detail: "Invalid role"}
			}
			v, err := scimOptionalString(scimFoldFields(role), "value")
			if err != nil {
				return user, err
			}
			user.Roles = append(user.Roles, v)
		}
	}
	return user, nil
}

type validSCIMUser struct {
	External, Handle, Name string
	Email                  *scimEmail
	Ref                    *samlUserRef
	RichInfo               richInfo
	Active                 bool
	Language, Role         *string
}

func validWireHandle(handle string) bool {
	if len(handle) < 2 || len(handle) > 256 {
		return false
	}
	for _, ch := range []byte(handle) {
		if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-' || ch == '.') {
			return false
		}
	}
	return true
}

// Native parseLanguage consumes two lowercase ISO letters without endOfInput.
const scimLanguages = "aa ab ae af ak am an ar as av ay az ba be bg bh bi bm bn bo br bs ca ce ch co cr cs cu cv cy da de dv dz ee el en eo es et eu fa ff fi fj fo fr fy ga gd gl gn gu gv ha he hi ho hr ht hu hy hz ia id ie ig ii ik io is it iu ja jv ka kg ki kj kk kl km kn ko kr ks ku kv kw ky la lb lg li ln lo lt lu lv mg mh mi mk ml mn mr ms mt my na nb nd ne ng nl nn no nr nv ny oc oj om or os pa pi pl ps pt qu rm rn ro ru rw sa sc sd se sg si sk sl sm sn so sq sr ss st su sv sw ta te tg th ti tk tl tn to tr ts tt tw ty ug uk ur uz ve vi vo wa wo xh yi yo za zh zu"

func validateSCIMModel(user scimUserInput, idp *idpConfig, limit int, operation string) (validSCIMUser, error) {
	var out validSCIMUser
	bad := func(message string) error {
		return &scimError{Status: 400, Type: "invalidValue", Detail: message + " (" + operation + ")"}
	}
	if user.Password != nil {
		return out, bad("Setting user passwords is not supported for security reasons.")
	}
	email, err := selectSCIMEmail(user.Emails)
	if err != nil {
		var e *scimError
		if errors.As(err, &e) {
			e.Detail += " (" + operation + ")"
		}
		return out, err
	}
	if user.External == nil {
		return out, &scimError{Status: 400, Type: "invalidValue", Detail: "externalId is required"}
	}
	out.External = *user.External
	if email == nil && scimEmailAddress(out.External) {
		email = &scimEmail{Value: out.External}
	}
	if idp == nil && email == nil {
		return out, &scimError{Status: 400, Type: "invalidValue", Detail: "Could not process externalId. Please check: (1) does the scim user contain a valid email address? (2) did you associate your scim token with a SAML IdP in wire?"}
	}
	if idp != nil {
		subject := samlNameID{Format: nameID11 + "unspecified", Value: out.External}
		if scimEmailAddress(out.External) {
			subject.Format = nameID11 + "emailAddress"
		}
		out.Ref = &samlUserRef{idp.Metadata.Issuer, subject}
	}
	out.Email = email
	if email != nil && email.Type != nil && utf8.RuneCountInString(*email.Type) > 64 {
		return out, bad("SCIM email type is too long (max 64 characters).")
	}
	out.Handle = strings.ToLower(user.UserName)
	if !validWireHandle(out.Handle) {
		return out, &scimError{Status: 400, Type: "invalidValue", Detail: out.Handle + "is not a valid Wire handle"}
	}
	out.Name = out.External
	if idp == nil && email != nil {
		out.Name = email.Value
	}
	if user.DisplayName != nil {
		out.Name = *user.DisplayName
	}
	if n := utf8.RuneCountInString(out.Name); n < 1 || n > 128 {
		return out, bad("Name: outside range [1, 128]")
	}
	out.RichInfo = user.RichInfo.normalized()
	if size := out.RichInfo.size(); size > limit {
		return out, &scimError{Status: 413, Type: "invalidValue", Detail: fmt.Sprintf("[%q,%q] together exceed the size limit: max %d characters, but got %d (%s)", richMapURN, richListURN, limit, size, operation)}
	}
	out.Active = user.Active == nil || *user.Active
	if user.Language != nil {
		value := *user.Language
		if len(value) < 2 || !strings.Contains(" "+scimLanguages+" ", " "+value[:2]+" ") {
			return out, bad("Could not parse language. Expected format is ISO 639-1.")
		}
		v := value[:2]
		out.Language = &v
	}
	if len(user.Roles) > 1 {
		return out, bad("A user cannot have more than one role.")
	}
	if len(user.Roles) == 1 {
		if user.Roles[0] == nil {
			return out, bad("A role must have a value.")
		}
		role := *user.Roles[0]
		if role != "owner" && role != "admin" && role != "member" && role != "partner" {
			return out, bad("The role '" + role + "' is not valid. Valid roles are owner, admin, member, partner.")
		}
		out.Role = &role
	}
	return out, nil
}
