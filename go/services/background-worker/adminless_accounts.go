// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"bytes"
	"encoding/json"
	"errors"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Validate the full native User schema before projecting eligibility. A bad
// otherwise-unused field makes native responseJsonMaybe discard the whole chunk.
func decodeAdminlessAccount(raw json.RawMessage) (adminlessUser, error) {
	var user adminlessUser
	invalid := errors.New("invalid Brig account response")
	obj, err := adminlessObject(raw)
	if err != nil {
		return user, invalid
	}
	text := func(field string) (string, error) {
		value, err := adminlessText(obj[field])
		if err != nil {
			return "", invalid
		}
		return value, nil
	}
	uuid := func(raw json.RawMessage) (string, error) {
		value, err := adminlessText(raw)
		if err != nil {
			return "", invalid
		}
		id, err := parseUserID(value)
		if err != nil {
			return "", invalid
		}
		return id.String(), nil
	}
	enum := func(field string, allowed ...string) (string, error) {
		value, err := text(field)
		if err != nil {
			return "", invalid
		}
		for _, a := range allowed {
			if value == a {
				return value, nil
			}
		}
		return "", invalid
	}
	qualified, err := adminlessObject(obj["qualified_id"])
	if err != nil {
		return user, invalid
	}
	user.ID, err = uuid(qualified["id"])
	if err != nil {
		return user, invalid
	}
	domain, err := adminlessText(qualified["domain"])
	if err != nil || !validDomain(domain) {
		return user, invalid
	}
	// Source optional(field id) ignores missing or malformed deprecated id; the
	// qualified id remains authoritative and no equality check is introduced.
	user.Type, err = enum("type", "regular", "app", "bot")
	if err != nil {
		return user, invalid
	}
	user.Status, err = enum("status", "active", "suspended", "deleted", "ephemeral", "pending-invitation")
	if err != nil {
		return user, invalid
	}
	user.Name, err = text("name")
	if err != nil || utf8.RuneCountInString(user.Name) < 1 || utf8.RuneCountInString(user.Name) > 128 {
		return user, invalid
	}
	if adminlessPresent(obj["text_status"]) {
		value, err := text("text_status")
		if err != nil || utf8.RuneCountInString(value) < 1 || utf8.RuneCountInString(value) > 256 {
			return user, invalid
		}
	}
	for _, field := range []string{"email", "email_unvalidated"} {
		if adminlessPresent(obj[field]) {
			value, err := text(field)
			if err != nil {
				return user, invalid
			}
			if _, err := canonicalUserMigrationEmail(value); err != nil {
				return user, invalid
			}
		}
	}
	if adminlessPresent(obj["sso_id"]) {
		if _, err := decodeUserMigrationSSO(string(obj["sso_id"])); err != nil {
			return user, invalid
		}
	}
	if !adminlessPresent(obj["accent_id"]) {
		return user, invalid
	}
	if !validAdminlessAccountColour(obj["accent_id"]) {
		return user, invalid
	}
	locale, err := text("locale")
	if err != nil {
		return user, invalid
	}
	// Native parseLocale uses parseOnly without endOfInput. Failed optional
	// country parsing backtracks, so any suffix after a valid language is accepted.
	if _, err := decodeUserMigrationLocale(locale, false); err != nil {
		return user, invalid
	}
	if adminlessPresent(obj["service"]) {
		service, err := adminlessObject(obj["service"])
		if err != nil {
			return user, invalid
		}
		for _, key := range []string{"id", "provider"} {
			if _, err := uuid(service[key]); err != nil {
				return user, invalid
			}
		}
		user.Service = true
	}
	if adminlessPresent(obj["team"]) {
		if _, err := uuid(obj["team"]); err != nil {
			return user, invalid
		}
	}
	if adminlessPresent(obj["handle"]) {
		value, err := text("handle")
		if err != nil || len(value) < 2 || len(value) > 256 {
			return user, invalid
		}
		for _, c := range value {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '.' || c == '-') {
				return user, invalid
			}
		}
	}
	if adminlessPresent(obj["expires_at"]) {
		value, err := text("expires_at")
		if err != nil || !validAdminlessAccountTime(value) {
			return user, invalid
		}
	}
	if adminlessPresent(obj["managed_by"]) {
		if _, err := enum("managed_by", "wire", "scim"); err != nil {
			return user, invalid
		}
	}
	for _, field := range []string{"deleted", "searchable"} {
		if adminlessPresent(obj[field]) {
			var value bool
			if json.Unmarshal(obj[field], &value) != nil {
				return user, invalid
			}
		}
	}
	if adminlessPresent(obj["picture"]) {
		var objects []json.RawMessage
		if json.Unmarshal(obj["picture"], &objects) != nil || len(objects) > 10 {
			return user, invalid
		}
		for _, raw := range objects {
			if _, err := adminlessObject(raw); err != nil {
				return user, invalid
			}
		}
	}
	if adminlessPresent(obj["assets"]) {
		var assets []json.RawMessage
		if json.Unmarshal(obj["assets"], &assets) != nil {
			return user, invalid
		}
		for _, raw := range assets {
			asset, err := adminlessObject(raw)
			if err != nil {
				return user, invalid
			}
			typ, err := adminlessText(asset["type"])
			if err != nil || typ != "image" {
				return user, invalid
			}
			key, err := adminlessText(asset["key"])
			if err != nil {
				return user, invalid
			}
			if _, ok := canonicalUserAssetKey(key); !ok {
				return user, invalid
			}
			if adminlessPresent(asset["size"]) {
				size, err := adminlessText(asset["size"])
				if err != nil || size != "complete" && size != "preview" {
					return user, invalid
				}
			}
		}
	}
	if adminlessPresent(obj["supported_protocols"]) {
		var protocols []json.RawMessage
		if json.Unmarshal(obj["supported_protocols"], &protocols) != nil {
			return user, invalid
		}
		for _, raw := range protocols {
			p, err := adminlessText(raw)
			if err != nil || p != "proteus" && p != "mls" {
				return user, invalid
			}
		}
	}
	return user, nil
}
func decodeAdminlessAccounts(body []byte) []adminlessUser {
	if !validAdminlessAccountJSONText(body) {
		return nil
	}
	var raws []json.RawMessage
	if json.Unmarshal(body, &raws) != nil {
		return nil
	}
	users := make([]adminlessUser, 0, len(raws))
	for _, raw := range raws {
		user, err := decodeAdminlessAccount(raw)
		if err != nil {
			return nil
		}
		users = append(users, user)
	}
	return users
}

// Primary attoparsec-iso8601 utcTime grammar includes signed/extended years,
// optional seconds, leap seconds and +HH[[:]MM], with no space before zone.
// This field is validation-only; no narrower Go time.Time conversion is needed.
var adminlessAccountTimePattern = regexp.MustCompile(`^([+-]?)([0-9]{4,})-([0-9]{2})-([0-9]{2})[T ]([0-9]{2}):([0-9]{2})(?::([0-9]{2})(?:\.([0-9]+))?)?(Z|[+-][0-9]{2}(?::?[0-9]{2})?)$`)

func validAdminlessAccountTime(value string) bool {
	m := adminlessAccountTimePattern.FindStringSubmatch(value)
	if m == nil {
		return false
	}
	integer := func(s string) int { n, _ := strconv.Atoi(s); return n }
	y := 0
	for _, digit := range m[2] {
		y = (y*10 + int(digit-'0')) % 400
	}
	month, day := integer(m[3]), integer(m[4])
	if month < 1 || month > 12 {
		return false
	}
	days := []int{31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}
	if y%4 == 0 && (y%100 != 0 || y%400 == 0) {
		days[1] = 29
	}
	if day < 1 || day > days[month-1] || integer(m[5]) > 23 || integer(m[6]) > 59 || integer(m[7]) > 60 {
		return false
	}
	zone := m[9]
	if zone == "Z" {
		return true
	}
	h := integer(zone[1:3])
	minutes := 0
	if len(zone) > 3 {
		suffix := zone[3:]
		if suffix[0] == ':' {
			suffix = suffix[1:]
		}
		minutes = integer(suffix)
	}
	if minutes > 59 {
		return false
	}
	offset := h*60 + minutes
	if zone[0] == '-' {
		offset = -offset
	}
	return offset >= -720 && offset <= 840
}

func validAdminlessAccountColour(raw json.RawMessage) bool {
	if !json.Valid(raw) {
		return false
	}
	m := adminlessWordPattern.FindStringSubmatch(string(bytes.TrimSpace(raw)))
	if m == nil {
		return false
	}
	digits := strings.TrimLeft(m[2]+m[3], "0")
	if digits == "" {
		return true
	}
	power := big.NewInt(int64(-len(m[3])))
	if m[4] != "" {
		e, ok := new(big.Int).SetString(strings.TrimPrefix(m[4], "+"), 10)
		if !ok {
			return false
		}
		power.Add(power, e)
	}
	trimmed := strings.TrimRight(digits, "0")
	power.Add(power, big.NewInt(int64(len(digits)-len(trimmed))))
	if power.Sign() < 0 || power.Cmp(big.NewInt(10)) > 0 || int64(len(trimmed))+power.Int64() > 10 {
		return false
	}
	n, _ := strconv.ParseInt(trimmed, 10, 64)
	for i := int64(0); i < power.Int64(); i++ {
		n *= 10
	}
	if m[1] == "-" {
		n = -n
	}
	return n >= -2147483648 && n <= 2147483647
}

// Aeson rejects unpaired escaped UTF16 surrogates; Go's JSON decoder otherwise
// replaces them. Preserve native whole-chunk failure, including unknown fields.
func validAdminlessAccountJSONText(body []byte) bool {
	if !utf8.Valid(body) {
		return false
	}
	for i := 0; i < len(body); i++ {
		if body[i] != '\\' {
			continue
		}
		if i+1 >= len(body) {
			return false
		}
		if body[i+1] != 'u' {
			i++
			continue
		}
		if i+6 > len(body) {
			return false
		}
		value, err := strconv.ParseUint(string(body[i+2:i+6]), 16, 16)
		if err != nil {
			return false
		}
		if value >= 0xdc00 && value <= 0xdfff {
			return false
		}
		if value >= 0xd800 && value <= 0xdbff {
			if i+12 > len(body) || body[i+6] != '\\' || body[i+7] != 'u' {
				return false
			}
			low, err := strconv.ParseUint(string(body[i+8:i+12]), 16, 16)
			if err != nil || low < 0xdc00 || low > 0xdfff {
				return false
			}
			i += 6
		}
		i += 5
	}
	return true
}
