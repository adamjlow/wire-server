// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
)

type passwordResetHTTP struct{ service passwordResetService }

func decodePasswordReset(reader io.Reader, begin bool, legacyKey *string) (passwordResetRequest, error) {
	var request passwordResetRequest
	invalid := errors.New("invalid reset request")
	var raw json.RawMessage
	decoder := json.NewDecoder(reader)
	var trailing any
	if decoder.Decode(&raw) != nil || decoder.Decode(&trailing) != io.EOF || !validRegistrationJSONText(raw) {
		return request, invalid
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return request, invalid
	}
	text := func(key string, required bool) (*string, error) {
		raw, present := fields[key]
		if !present || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			if required {
				return nil, invalid
			}
			return nil, nil
		}
		var value string
		if json.Unmarshal(raw, &value) != nil {
			return nil, invalid
		}
		return &value, nil
	}
	var err error
	if legacyKey != nil {
		request.key = legacyKey
	} else {
		if request.email, err = text("email", false); err != nil {
			return request, err
		}
		if request.email != nil {
			email, err := canonicalRegistrationEmail(*request.email)
			if err != nil {
				return request, invalid
			}
			request.email = &email
		}
		if request.phone, err = text("phone", false); err != nil {
			return request, err
		}
		if begin {
			if (request.email == nil) == (request.phone == nil) {
				return request, invalid
			}
			return request, nil
		}
		if request.key, err = text("key", false); err != nil {
			return request, err
		}
		if request.key != nil && !validActivationValue(*request.key) {
			return request, invalid
		}
		if request.phone != nil {
			// Actual native parser accepts '+' and8–15 ASCII digits, despite
			// the nearby source comment saying10–15.
			phone := *request.phone
			if len(phone) < 9 || len(phone) > 16 || phone[0] != '+' {
				return request, invalid
			}
			for _, digit := range phone[1:] {
				if digit < '0' || digit > '9' {
					return request, invalid
				}
			}
		}
		if request.key == nil && request.email == nil && request.phone == nil {
			return request, invalid
		}
	}
	code, err := text("code", true)
	if err != nil || !validActivationValue(*code) {
		return request, invalid
	}
	password, err := text("password", true)
	if err != nil || validatePlainPassword(*password, 8) != nil {
		return request, invalid
	}
	request.code, request.password = *code, *password
	return request, nil
}

func passwordResetHTTPError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errPasswordResetKey):
		authenticationWireError(w, 400, "invalid-key", "Invalid email or mobile number for password reset.")
	case errors.Is(err, errPasswordResetCode):
		authenticationWireError(w, 400, "invalid-code", "Invalid password reset code.")
	case errors.Is(err, errPasswordResetSame):
		authenticationWireError(w, 409, "password-must-differ", "For password reset, new and old password must be different.")
	case errors.Is(err, errPasswordResetPhone):
		authenticationWireError(w, 400, "invalid-phone", "Invalid mobile phone number")
	default:
		registrationHTTPError(w, err)
	}
}

func (h passwordResetHTTP) handle(w http.ResponseWriter, r *http.Request, path string) bool {
	internal := path == "/i/users/password-reset-code"
	begin := path == "/password-reset"
	complete := path == "/password-reset/complete"
	legacy := strings.HasPrefix(path, "/password-reset/") && strings.Count(path, "/") == 2 && !complete
	if !internal && !begin && !complete && !legacy {
		return false
	}
	if internal {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(405)
			return true
		}
		values, present := r.URL.Query()["email"]
		if !present || len(values) == 0 {
			routeParseError(w, "Query parameter email is required")
			return true
		}
		email, err := canonicalRegistrationEmail(values[0])
		if err != nil {
			routeParseError(w, "Error parsing query parameter email failed: Invalid email")
			return true
		}
		pair, err := h.service.pair(r.Context(), email)
		if err != nil {
			passwordResetHTTPError(w, err)
		} else if pair == nil {
			passwordResetHTTPError(w, errPasswordResetKey)
		} else {
			readJSON(w, r, pair)
		}
		return true
	}
	var legacyKey *string
	if legacy {
		version, _ := strconv.Atoi(r.Header.Get("X-Wire-API-Version"))
		if version >= 8 {
			return false
		}
		key := strings.TrimPrefix(path, "/password-reset/")
		if key == "" {
			return false
		}
		if !validActivationValue(key) {
			routeParseError(w, "Error parsing URL piece failed: Invalid url-safe base-64 characters")
			return true
		}
		legacyKey = &key
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return true
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(media, "application/json") {
		w.WriteHeader(415)
		return true
	}
	request, err := decodePasswordReset(r.Body, begin, legacyKey)
	if err != nil {
		wireError(w, 400, "bad-request", "Invalid password reset request")
		return true
	}
	if begin {
		err = h.service.begin(r.Context(), request)
	} else {
		err = h.service.complete(r.Context(), request)
	}
	if err != nil {
		passwordResetHTTPError(w, err)
	} else if begin {
		w.WriteHeader(201)
	} else {
		w.WriteHeader(200)
	}
	return true
}
