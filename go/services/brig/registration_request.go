// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type registrationTeam struct {
	Name              string
	Icon              string
	IconKey, Currency *string
}
type registrationOrigin struct {
	InvitationCode, TeamCode *string
	Team                     *registrationTeam
	SSOTeam                  *gocql.UUID
}
type registrationRequest struct {
	User                       newRegistrationUser
	Password, EmailCode, Label *string
	Origin                     registrationOrigin
}

func (registrationRequest) String() string { return "<RegistrationRequest>" }
func invalidRegistrationRequest() error    { return errors.New("invalid registration request") }
func registrationSchemaText(value any) (string, error) {
	text, ok := value.(string)
	if !ok || !utf8.ValidString(text) {
		return "", invalidRegistrationRequest()
	}
	return text, nil
}
func registrationPresent(raw json.RawMessage) bool {
	return len(raw) > 0 && !bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}
func registrationObject(raw json.RawMessage) (map[string]json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil || obj == nil {
		return nil, invalidRegistrationRequest()
	}
	return obj, nil
}
func registrationText(obj map[string]json.RawMessage, field string, required bool) (*string, error) {
	raw := obj[field]
	if !registrationPresent(raw) {
		if required {
			return nil, invalidRegistrationRequest()
		}
		return nil, nil
	}
	var text string
	if json.Unmarshal(raw, &text) != nil {
		return nil, invalidRegistrationRequest()
	}
	return &text, nil
}
func registrationUUID(value string) (gocql.UUID, error) {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return gocql.UUID{}, invalidRegistrationRequest()
	}
	return gocql.ParseUUID(value)
}

// Parse the entire native NewUserRaw schema before deriving identity/origin and
// applying public restrictions. Unknown fields are accepted by native Aeson.
func decodeRegistration(reader io.Reader, public bool) (registrationRequest, error) {
	var raw json.RawMessage
	decoder := json.NewDecoder(reader)
	var trailing any
	if decoder.Decode(&raw) != nil || decoder.Decode(&trailing) != io.EOF || !validRegistrationJSONText(raw) {
		return registrationRequest{}, invalidRegistrationRequest()
	}
	obj, err := registrationObject(raw)
	if err != nil {
		return registrationRequest{}, err
	}
	var request registrationRequest
	n := &request.User
	name, err := registrationText(obj, "name", true)
	if err != nil || utf8.RuneCountInString(*name) < 1 || utf8.RuneCountInString(*name) > 128 {
		return request, invalidRegistrationRequest()
	}
	n.Name = *name
	for _, field := range []string{"uuid", "team_id"} {
		value, err := registrationText(obj, field, false)
		if err != nil {
			return request, err
		}
		if value != nil {
			id, err := registrationUUID(*value)
			if err != nil {
				return request, invalidRegistrationRequest()
			}
			if field == "uuid" {
				n.UUID = &id
			} else {
				request.Origin.SSOTeam = &id
			}
		}
	}
	n.Email, err = registrationText(obj, "email", false)
	if err != nil {
		return request, err
	}
	if n.Email != nil {
		canonical, err := canonicalRegistrationEmail(*n.Email)
		if err != nil {
			return request, err
		}
		n.Email = &canonical
	}
	if registrationPresent(obj["sso_id"]) {
		n.SSO, err = decodeRegistrationSSO(string(obj["sso_id"]))
		if err != nil {
			return request, invalidRegistrationRequest()
		}
	}
	if registrationPresent(obj["picture"]) {
		if json.Unmarshal(obj["picture"], &n.Picture) != nil || len(n.Picture) > 10 {
			return request, invalidRegistrationRequest()
		}
		for _, picture := range n.Picture {
			if _, err := registrationObject(picture); err != nil {
				return request, err
			}
		}
	}
	n.Assets, err = registrationAssets(obj["assets"])
	if err != nil {
		return request, err
	}
	if registrationPresent(obj["accent_id"]) {
		value, err := registrationInteger(obj["accent_id"])
		if err != nil {
			return request, err
		}
		accent := int32(value)
		n.Accent = &accent
	}
	for field, target := range map[string]**string{"email_code": &request.EmailCode, "invitation_code": &request.Origin.InvitationCode, "team_code": &request.Origin.TeamCode, "label": &request.Label, "password": &request.Password} {
		*target, err = registrationText(obj, field, false)
		if err != nil {
			return request, err
		}
		if field != "label" && field != "password" && *target != nil && !validActivationValue(**target) {
			return request, invalidRegistrationRequest()
		}
	}
	if request.Password != nil && validatePlainPassword(*request.Password, 8) != nil {
		return request, invalidRegistrationRequest()
	}
	if registrationPresent(obj["team"]) {
		request.Origin.Team, err = decodeRegistrationTeam(obj["team"])
		if err != nil {
			return request, err
		}
	}
	locale, err := registrationText(obj, "locale", false)
	if err != nil {
		return request, err
	}
	if locale != nil {
		value, err := parseRegistrationLocale(*locale)
		if err != nil {
			return request, err
		}
		n.Locale = &value
	}
	if registrationPresent(obj["expires_in"]) {
		value, err := registrationInteger(obj["expires_in"])
		if err != nil || value < 1 || value > 604800 {
			return request, invalidRegistrationRequest()
		}
		n.ExpiresIn = &value
	}
	managed, err := registrationText(obj, "managed_by", false)
	if err != nil {
		return request, err
	}
	if managed != nil {
		value := int32(0)
		switch *managed {
		case "wire":
		case "scim":
			value = 1
		default:
			return request, invalidRegistrationRequest()
		}
		n.ManagedBy = &value
	}
	if registrationPresent(obj["supported_protocols"]) {
		var protocols []string
		if json.Unmarshal(obj["supported_protocols"], &protocols) != nil {
			return request, invalidRegistrationRequest()
		}
		value := int32(0)
		for _, protocol := range protocols {
			switch protocol {
			case "proteus":
				value |= 1
			case "mls":
				value |= 2
			default:
				return request, invalidRegistrationRequest()
			}
		}
		n.Protocols = &value
	}
	if err := request.validateOrigin(); err != nil {
		return request, err
	}
	if n.ExpiresIn != nil && (n.Email != nil || len(n.SSO) > 0) {
		return request, errors.New("Only users without an identity can expire")
	}
	if public {
		switch {
		case len(n.SSO) > 0:
			return request, errors.New("SSO-managed users are not allowed here.")
		case n.UUID != nil:
			return request, errors.New("it is not allowed to provide a UUID for the users here.")
		case n.ManagedBy != nil && *n.ManagedBy != 0:
			return request, errors.New("only managed-by-Wire users can be created here.")
		}
	}
	return request, nil
}

func (r registrationRequest) validateOrigin() error {
	o := r.Origin
	sso := len(r.User.SSO) > 0
	count := 0
	for _, present := range []bool{o.InvitationCode != nil, o.TeamCode != nil, o.Team != nil, sso, o.SSOTeam != nil} {
		if present {
			count++
		}
	}
	switch {
	case sso && o.SSOTeam == nil || !sso && o.SSOTeam != nil:
		return errors.New("sso_id, team_id must be either both present or both absent.")
	case sso && count != 2 || !sso && count > 1:
		return errors.New("team_code, team, invitation_code, sso_id, and the pair (sso_id, team_id) are mutually exclusive")
	case !sso && (o.TeamCode != nil || o.Team != nil) && r.Password == nil:
		return errors.New("all team users must set a password on creation")
	}
	return nil
}

func parseRegistrationLocale(value string) (registrationLocale, error) {
	if len(value) < 2 || !strings.Contains(nativeUserLanguages, " "+value[:2]+" ") {
		return registrationLocale{}, invalidRegistrationRequest()
	}
	locale := registrationLocale{Language: value[:2]}
	// Native optional country parser backtracks; parseOnly does not require EOF.
	if len(value) >= 5 && value[2] == '-' && strings.Contains(nativeUserCountries, " "+value[3:5]+" ") {
		country := value[3:5]
		locale.Country = &country
	}
	return locale, nil
}

func decodeRegistrationTeam(raw json.RawMessage) (*registrationTeam, error) {
	obj, err := registrationObject(raw)
	if err != nil {
		return nil, err
	}
	name, err := registrationText(obj, "name", true)
	if err != nil || utf8.RuneCountInString(*name) < 1 || utf8.RuneCountInString(*name) > 256 {
		return nil, invalidRegistrationRequest()
	}
	icon, err := registrationText(obj, "icon", true)
	if err != nil {
		return nil, err
	}
	if *icon != "default" {
		canonical, ok := canonicalRegistrationAssetKey(*icon)
		if !ok {
			return nil, invalidRegistrationRequest()
		}
		icon = &canonical
	}
	key, err := registrationText(obj, "icon_key", false)
	if err != nil || key != nil && (utf8.RuneCountInString(*key) < 1 || utf8.RuneCountInString(*key) > 256) {
		return nil, invalidRegistrationRequest()
	}
	currency, err := registrationText(obj, "currency", false)
	if err != nil || currency != nil && (len(*currency) != 3 || !strings.Contains(nativeRegistrationCurrencies, " "+*currency+" ")) {
		return nil, invalidRegistrationRequest()
	}
	return &registrationTeam{Name: *name, Icon: *icon, IconKey: key, Currency: currency}, nil
}

func registrationAssets(raw json.RawMessage) ([]userAsset, error) {
	assets := []userAsset{}
	if !registrationPresent(raw) {
		return assets, nil
	}
	var values []json.RawMessage
	if json.Unmarshal(raw, &values) != nil {
		return nil, invalidRegistrationRequest()
	}
	for _, value := range values {
		obj, err := registrationObject(value)
		if err != nil {
			return nil, err
		}
		typ, err := registrationText(obj, "type", true)
		if err != nil || *typ != "image" {
			return nil, invalidRegistrationRequest()
		}
		key, err := registrationText(obj, "key", true)
		if err != nil {
			return nil, err
		}
		canonical, ok := canonicalRegistrationAssetKey(*key)
		if !ok {
			return nil, invalidRegistrationRequest()
		}
		size, err := registrationText(obj, "size", false)
		if err != nil {
			return nil, err
		}
		asset := userAsset{Key: canonical}
		if size != nil {
			v := int32(0)
			switch *size {
			case "preview":
			case "complete":
				v = 1
			default:
				return nil, invalidRegistrationRequest()
			}
			asset.Size = &v
		}
		assets = append(assets, asset)
	}
	return assets, nil
}

func canonicalRegistrationAssetKey(value string) (string, bool) {
	version, rest, ok := strings.Cut(value, "-")
	if !ok {
		return "", false
	}
	retention, id, ok := strings.Cut(rest, "-")
	if !ok {
		return "", false
	}
	v, ok := nativeRegistrationAssetWord(version)
	if !ok || v != 3 {
		return "", false
	}
	r, ok := nativeRegistrationAssetWord(retention)
	if !ok || r < 1 || r > 5 {
		return "", false
	}
	u, err := registrationUUID(id)
	if err != nil {
		return "", false
	}
	return "3-" + strconv.FormatUint(r, 10) + "-" + u.String(), true
}
func nativeRegistrationAssetWord(value string) (uint64, bool) {
	if value == "" {
		return 0, false
	}
	var result uint64
	for _, c := range value {
		if c < '0' || c > '9' {
			return 0, false
		}
		result = result*10 + uint64(c-'0')
	}
	return result, true // Native decimal Word arithmetic wraps at 64 bits.
}

var registrationNumberPattern = regexp.MustCompile(`^(-?)([0-9]+)(?:\.([0-9]+))?(?:[eE]([+-]?[0-9]+))?$`)

// Bounded integral conversion preserves Scientific decimal/exponent input
// without allocating an enormous power of ten for adversarial exponents.
func registrationInteger(raw json.RawMessage) (int64, error) {
	m := registrationNumberPattern.FindStringSubmatch(string(bytes.TrimSpace(raw)))
	if m == nil {
		return 0, invalidRegistrationRequest()
	}
	digits := strings.TrimLeft(m[2]+m[3], "0")
	if digits == "" {
		return 0, nil
	}
	power := big.NewInt(int64(-len(m[3])))
	if m[4] != "" {
		exponent, ok := new(big.Int).SetString(strings.TrimPrefix(m[4], "+"), 10)
		if !ok {
			return 0, invalidRegistrationRequest()
		}
		power.Add(power, exponent)
	}
	trimmed := strings.TrimRight(digits, "0")
	power.Add(power, big.NewInt(int64(len(digits)-len(trimmed))))
	if power.Sign() < 0 || power.Cmp(big.NewInt(10)) > 0 || int64(len(trimmed))+power.Int64() > 10 {
		return 0, invalidRegistrationRequest()
	}
	n, _ := strconv.ParseInt(trimmed, 10, 64)
	for i := int64(0); i < power.Int64(); i++ {
		n *= 10
	}
	if m[1] == "-" {
		n = -n
	}
	if n < -2147483648 || n > 2147483647 {
		return 0, invalidRegistrationRequest()
	}
	return n, nil
}

// Aeson rejects unpaired escaped UTF16 surrogates; Go's JSON decoder otherwise
// replaces them. Preserve native whole-chunk failure, including unknown fields.
func validRegistrationJSONText(body []byte) bool {
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
