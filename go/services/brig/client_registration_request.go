// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

func decodeClientRegistration(reader io.Reader, version int) (clientRegistrationRequest, error) {
	var request clientRegistrationRequest
	invalid := errors.New("invalid client registration request")
	var raw json.RawMessage
	var extra any
	decoder := json.NewDecoder(reader)
	if decoder.Decode(&raw) != nil || decoder.Decode(&extra) != io.EOF {
		return request, invalid
	}
	parsed, err := parsePropertyValue(raw)
	if err != nil {
		return request, invalid
	}
	canonical, err := json.Marshal(parsed)
	if err != nil {
		return request, invalid
	}
	fields, err := registrationObject(canonical)
	if err != nil {
		return request, invalid
	}
	// Unlike UpdateClient, these three fields are required by NewClient.
	if !registrationPresent(fields["prekeys"]) || !registrationPresent(fields["lastkey"]) {
		return request, invalid
	}
	update, err := decodeClientUpdate(bytes.NewReader(canonical), version)
	if err != nil || update.last == nil {
		return request, invalid
	}
	request.last, request.prekeys = *update.last, update.prekeys
	request.client.Label, request.client.PublicKeys = update.label, update.publicKeys
	if update.caps != nil {
		request.client.Capabilities = *update.caps
	}
	typ, err := registrationText(fields, "type", true)
	if err != nil {
		return request, invalid
	}
	switch *typ {
	case "temporary":
		request.client.Type = 0
	case "permanent":
		request.client.Type = 1
	case "legalhold":
		request.client.Type = 2
	default:
		return request, invalid
	}
	class, err := registrationText(fields, "class", false)
	if err != nil {
		return request, invalid
	}
	if class != nil {
		var n int32
		switch *class {
		case "phone":
			n = 0
		case "tablet":
			n = 1
		case "desktop":
			n = 2
		case "legalhold":
			n = 3
		default:
			return request, invalid
		}
		request.client.Class = &n
	}
	request.client.Cookie, err = registrationText(fields, "cookie", false)
	if err != nil {
		return request, invalid
	}
	request.client.Model, err = registrationText(fields, "model", false)
	if err != nil {
		return request, invalid
	}
	request.password, err = registrationText(fields, "password", false)
	if err != nil || request.password != nil && validatePlainPassword(*request.password, 6) != nil {
		return request, invalid
	}
	request.verificationCode, err = registrationText(fields, "verification_code", false)
	if err != nil || request.verificationCode != nil && !validVerificationValue(*request.verificationCode) {
		return request, invalid
	}
	return request, nil
}
