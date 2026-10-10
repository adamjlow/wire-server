// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
)

type scimAttr struct{ Schema, Name, Sub string }
type scimComparison struct {
	Attr  scimAttr
	Op    string
	Value any
}

var scimAttrName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*`)

func scimSpace(b byte) bool { return b == ' ' || b >= '\t' && b <= '\r' }
func scimTrimSpace(s string) string {
	return strings.TrimFunc(s, func(r rune) bool { return r < 128 && scimSpace(byte(r)) })
}
func scimParseAttr(s string, schemas []string) (scimAttr, string, error) {
	var attr scimAttr
	for _, schema := range schemas {
		if strings.HasPrefix(s, schema+":") {
			attr.Schema = schema
			s = strings.TrimPrefix(s, schema+":")
			break
		}
	}
	name := scimAttrName.FindString(s)
	if name == "" {
		return attr, s, errors.New("invalid SCIM attribute")
	}
	attr.Name = name
	s = s[len(name):]
	if strings.HasPrefix(s, ".") {
		s = s[1:]
		attr.Sub = scimAttrName.FindString(s)
		if attr.Sub == "" {
			return attr, s, errors.New("invalid SCIM sub-attribute")
		}
		s = s[len(attr.Sub):]
	}
	return attr, s, nil
}
func scimParseComparison(s string, schemas []string) (scimComparison, error) {
	var result scimComparison
	if s == "" || scimSpace(s[0]) || scimSpace(s[len(s)-1]) {
		return result, errors.New("invalid SCIM comparison whitespace")
	}
	attr, rest, err := scimParseAttr(s, schemas)
	if err != nil {
		return result, err
	}
	result.Attr = attr
	if len(rest) == 0 || !scimSpace(rest[0]) {
		return result, errors.New("invalid SCIM comparison")
	}
	rest = strings.TrimLeftFunc(rest, func(r rune) bool { return r < 128 && scimSpace(byte(r)) })
	if len(rest) < 3 {
		return result, errors.New("invalid SCIM comparison")
	}
	result.Op = strings.ToLower(rest[:2])
	rest = rest[2:]
	if !strings.Contains(" eq ne co sw ew gt ge lt le ", " "+result.Op+" ") || len(rest) == 0 || !scimSpace(rest[0]) {
		return result, errors.New("invalid SCIM comparison")
	}
	rest = strings.TrimLeftFunc(rest, func(r rune) bool { return r < 128 && scimSpace(byte(r)) })
	if strings.EqualFold(rest, "true") {
		result.Value = true
		return result, nil
	}
	if strings.EqualFold(rest, "false") {
		result.Value = false
		return result, nil
	}
	d := json.NewDecoder(strings.NewReader(rest))
	d.UseNumber()
	if d.Decode(&result.Value) != nil {
		return result, errors.New("invalid SCIM literal")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return result, errors.New("invalid SCIM literal")
	}
	switch result.Value.(type) {
	case nil, bool, string, json.Number:
		return result, nil
	default:
		return result, errors.New("invalid SCIM literal")
	}
}
func scimCompareText(op, a, b string) bool {
	switch op {
	case "eq":
		return a == b
	case "ne":
		return a != b
	case "co":
		return strings.Contains(a, b)
	case "sw":
		return strings.HasPrefix(a, b)
	case "ew":
		return strings.HasSuffix(a, b)
	case "gt":
		return a > b
	case "ge":
		return a >= b
	case "lt":
		return a < b
	case "le":
		return a <= b
	}
	return false
}
