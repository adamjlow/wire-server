// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"sort"
	"strings"

	"golang.org/x/text/cases"
)

const scimPatchSchema = "urn:ietf:params:scim:api:messages:2.0:PatchOp"

type scimPatchPath struct {
	Attr   scimAttr
	Filter *scimComparison
	Sub    string
}
type scimPatchOperation struct {
	Op       string
	Path     *scimPatchPath
	Value    any
	HasValue bool
}

func scimPatchError(typ, detail string) error {
	return &scimError{Status: 400, Type: typ, Detail: detail}
}
func parseSCIMPatchPath(path string) (scimPatchPath, error) {
	var out scimPatchPath
	attr, rest, err := scimParseAttr(path, []string{scimUserSchema, richMapURN, richListURN})
	if err != nil {
		return out, err
	}
	out.Attr = attr
	if strings.HasPrefix(rest, "[") {
		// Find the closing bracket outside JSON strings, including escaped quotes.
		quoted, escaped, end := false, false, -1
		for i := 1; i < len(rest); i++ {
			b := rest[i]
			if escaped {
				escaped = false
				continue
			}
			if quoted && b == '\\' {
				escaped = true
				continue
			}
			if b == '"' {
				quoted = !quoted
				continue
			}
			if !quoted && b == ']' {
				end = i
				break
			}
		}
		if end < 0 {
			return out, scimPatchError("invalidSyntax", "Invalid patch path")
		}
		filter, err := scimParseComparison(rest[1:end], []string{scimUserSchema, richMapURN, richListURN})
		if err != nil {
			return out, err
		}
		out.Filter = &filter
		rest = rest[end+1:]
		if strings.HasPrefix(rest, ".") {
			out.Sub = scimAttrName.FindString(rest[1:])
			if out.Sub == "" {
				return out, scimPatchError("invalidSyntax", "Invalid patch path")
			}
			rest = rest[len(out.Sub)+1:]
		}
	}
	if rest != "" {
		return out, scimPatchError("invalidSyntax", "Invalid patch path")
	}
	return out, nil
}
func parseSCIMPatch(object map[string]any) ([]scimPatchOperation, error) {
	bad := scimPatchError("invalidSyntax", "Invalid PATCH request")
	object = scimFoldFields(object)
	schemas, ok := object["schemas"].([]any)
	if !ok {
		return nil, bad
	}
	supported := false
	for _, v := range schemas {
		s, ok := v.(string)
		if !ok {
			return nil, bad
		}
		supported = supported || s == scimPatchSchema
	}
	if !supported {
		return nil, bad
	}
	values, ok := object["operations"].([]any)
	if !ok {
		return nil, bad
	}
	ops := []scimPatchOperation{}
	for _, v := range values {
		obj, ok := v.(map[string]any)
		if !ok {
			return nil, bad
		}
		obj = scimFoldFields(obj)
		op, ok := obj["op"].(string)
		if !ok {
			return nil, bad
		}
		op = cases.Fold().String(op)
		if op != "add" && op != "replace" && op != "remove" {
			return nil, bad
		}
		value, exists := obj["value"]
		operation := scimPatchOperation{Op: op, Value: value, HasValue: exists}
		if value, exists := obj["path"]; exists && value != nil {
			path, ok := value.(string)
			if !ok {
				return nil, bad
			}
			parsed, err := parseSCIMPatchPath(path)
			if err != nil {
				return nil, bad
			}
			operation.Path = &parsed
		}
		ops = append(ops, operation)
	}
	return ops, nil
}

func scimEmailMatches(filter scimComparison, email scimEmail) bool {
	switch strings.ToLower(filter.Attr.Name) {
	case "type":
		value, ok := filter.Value.(string)
		if !ok {
			return false
		}
		typ := ""
		if email.Type != nil {
			typ = *email.Type
		}
		return scimCompareText(filter.Op, typ, value)
	case "value":
		value, ok := filter.Value.(string)
		return ok && scimCompareText(filter.Op, email.Value, value)
	case "primary":
		value, ok := filter.Value.(bool)
		if !ok {
			return false
		}
		match := email.Primary != nil && *email.Primary == value
		if filter.Op == "eq" {
			return match
		}
		if filter.Op == "ne" {
			return !match
		}
	}
	return false
}
func scimPatchEmails(value any) ([]scimEmail, error) {
	if _, ok := value.([]any); !ok {
		value = []any{value}
	}
	emails, err := parseSCIMEmails(map[string]any{"emails": value})
	if err != nil {
		return nil, scimPatchError("invalidValue", "Invalid email")
	}
	return emails, nil
}
func scimPatchEmailPath(user scimUserInput, operation scimPatchOperation) (scimUserInput, error) {
	path := *operation.Path
	filter := *path.Filter
	sub := strings.ToLower(path.Sub)
	badPath := func(detail string) (scimUserInput, error) { return user, scimPatchError("invalidPath", detail) }
	if !strings.EqualFold(path.Attr.Name, "emails") {
		return badPath("multi-valued PATCH is only supported for 'emails'")
	}
	if operation.Op == "remove" && sub == "value" {
		return badPath("removing the 'value' sub-attribute of 'emails' is not supported; remove the whole entry instead")
	}
	if operation.Op == "remove" && sub == "" {
		kept := []scimEmail{}
		for _, email := range user.Emails {
			if !scimEmailMatches(filter, email) {
				kept = append(kept, email)
			}
		}
		user.Emails = kept
		return user, nil
	}
	if operation.Op != "remove" && !operation.HasValue {
		return user, scimPatchError("invalidValue", "No value was provided")
	}
	value := operation.Value
	if operation.Op == "remove" {
		value = nil
	}
	if sub == "" {
		emails, err := scimPatchEmails(value)
		if err != nil {
			return user, err
		}
		if operation.Op == "add" {
			user.Emails = append(user.Emails, emails...)
			return user, nil
		}
		// Native foldr: process replacement entries from right to left.
		for i := len(emails) - 1; i >= 0; i-- {
			matched := false
			for j := range user.Emails {
				if scimEmailMatches(filter, user.Emails[j]) {
					user.Emails[j] = emails[i]
					matched = true
				}
			}
			if !matched {
				user.Emails = append(user.Emails, emails[i])
			}
		}
		return user, nil
	}
	if sub != "value" && sub != "type" && sub != "primary" {
		return badPath("only the 'value', 'type' and 'primary' sub-attributes of 'emails' can be patched")
	}
	if sub == "value" && value == nil {
		return badPath("setting the 'value' sub-attribute of 'emails' to null is not supported; remove the whole entry instead")
	}
	var text *string
	var primary *bool
	var err error
	if sub == "primary" {
		primary, err = scimOptionalBool(map[string]any{"primary": value}, "primary")
	} else {
		text, err = scimOptionalString(map[string]any{"value": value}, "value")
	}
	if err != nil || sub == "value" && !scimEmailAddress(*text) {
		return user, scimPatchError("invalidValue", "Invalid email "+sub)
	}
	matched := false
	for i := range user.Emails {
		if scimEmailMatches(filter, user.Emails[i]) {
			matched = true
			switch sub {
			case "value":
				user.Emails[i].Value = *text
			case "type":
				user.Emails[i].Type = text
			case "primary":
				user.Emails[i].Primary = primary
			}
		}
	}
	if !matched && sub == "value" && filter.Op == "eq" && strings.EqualFold(filter.Attr.Name, "type") {
		if typ, ok := filter.Value.(string); ok {
			user.Emails = append(user.Emails, scimEmail{Value: *text, Type: &typ})
		}
	}
	return user, nil
}
func scimPatchRich(user scimUserInput, operation scimPatchOperation) (scimUserInput, error) {
	path := *operation.Path
	if path.Attr.Sub != "" {
		return user, scimPatchError("invalidValue", "invalid patch op for rich info")
	}
	key := cases.Fold().String(path.Attr.Name)
	out := richInfo{}
	found := false
	text, ok := operation.Value.(string)
	if operation.Op != "remove" && (!operation.HasValue || !ok) {
		return user, scimPatchError("invalidValue", "rich info values can only be text")
	}
	for _, field := range user.RichInfo {
		if cases.Fold().String(field.Type) == key {
			found = true
			if operation.Op != "remove" {
				out = append(out, richField{path.Attr.Name, text})
			}
		} else {
			out = append(out, field)
		}
	}
	if !found && operation.Op != "remove" {
		out = append(out, richField{path.Attr.Name, text})
	}
	if path.Attr.Schema == richMapURN {
		sort.Slice(out, func(i, j int) bool { return cases.Fold().String(out[i].Type) < cases.Fold().String(out[j].Type) })
	}
	user.RichInfo = out.normalized()
	return user, nil
}

func applySCIMPatch(user scimUserInput, operations []scimPatchOperation) (scimUserInput, error) {
	for _, operation := range operations {
		if operation.Path != nil && operation.Path.Filter != nil {
			var err error
			user, err = scimPatchEmailPath(user, operation)
			if err != nil {
				return user, err
			}
			continue
		}
		if operation.Path != nil && (operation.Path.Attr.Schema == richMapURN || operation.Path.Attr.Schema == richListURN) {
			var err error
			user, err = scimPatchRich(user, operation)
			if err != nil {
				return user, err
			}
			continue
		}
		if operation.Op == "remove" {
			if operation.Path == nil {
				return user, scimPatchError("noTarget", "")
			}
			switch strings.ToLower(operation.Path.Attr.Name) {
			case "username":
				return user, scimPatchError("mutability", "")
			case "displayname":
				user.DisplayName = nil
			case "externalid":
				user.External = nil
			case "active":
				user.Active = nil
			case "roles":
				user.Roles = nil
			case "emails":
				if operation.Path.Attr.Sub != "" {
					return user, scimPatchError("invalidPath", `removing a sub-attribute of 'emails' requires a value-path filter, e.g. emails[type eq \"work\"].type`)
				}
				user.Emails = nil
			}
			continue
		}
		if !operation.HasValue {
			return user, scimPatchError("invalidValue", "No value was provided")
		}
		if operation.Path == nil {
			object, ok := operation.Value.(map[string]any)
			if !ok {
				return user, scimPatchError("invalidPath", "we only support attributes username, displayname, externalid, active, roles")
			}
			for key := range object {
				folded := cases.Fold().String(key)
				if folded != "username" && folded != "displayname" && folded != "externalid" && folded != "active" && folded != "roles" {
					return user, scimPatchError("invalidPath", "we only support attributes username, displayname, externalid, active, roles")
				}
			}
			value, err := parseSCIMUser(object)
			if err != nil {
				return user, scimPatchError("invalidValue", "Invalid replacement user")
			}
			user.UserName = value.UserName
			user.DisplayName = value.DisplayName
			user.External = value.External
			user.Active = value.Active
			continue
		}
		attr := strings.ToLower(operation.Path.Attr.Name)
		var err error
		switch attr {
		case "username":
			value, ok := operation.Value.(string)
			if !ok {
				err = scimPatchError("invalidValue", "Invalid userName")
			} else {
				user.UserName = value
			}
		case "displayname":
			user.DisplayName, err = scimOptionalString(map[string]any{attr: operation.Value}, attr)
		case "externalid":
			user.External, err = scimOptionalString(map[string]any{attr: operation.Value}, attr)
		case "active":
			user.Active, err = scimOptionalBool(map[string]any{attr: operation.Value}, attr)
		case "roles":
			value, parseErr := parseSCIMUser(map[string]any{"userName": user.UserName, "roles": operation.Value})
			user.Roles = value.Roles
			err = parseErr
		default:
			err = scimPatchError("invalidPath", "we only support attributes username, displayname, externalid, active, roles")
		}
		if err != nil {
			if failure, ok := err.(*scimError); ok && failure.Type == "invalidSyntax" {
				failure.Type = "invalidValue"
			}
			return user, err
		}
	}
	return user, nil
}
