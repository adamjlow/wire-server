// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func mlsSuiteHex(text string) (uint16, bool) {
	text = strings.TrimPrefix(text, "0x")
	if text == "" {
		return 0, false
	}
	var value uint16
	for _, ch := range text {
		var digit uint16
		switch {
		case ch >= '0' && ch <= '9':
			digit = uint16(ch - '0')
		case ch >= 'a' && ch <= 'f':
			digit = uint16(ch - 'a' + 10)
		case ch >= 'A' && ch <= 'F':
			digit = uint16(ch - 'A' + 10)
		default:
			return 0, false
		}
		value = value*16 + digit // native hexadecimal Word16 accumulator wraps
	}
	return value, true
}
func (s mlsClientReads) handle(w http.ResponseWriter, r *http.Request, path string) bool {
	if s.handlePublicCount(w, r, path) {
		return true
	}
	federation := path == "/federation/get-mls-clients" || path == "/federation/get-mls-client"
	single := path == "/federation/get-mls-client"
	var userText, client string
	var suite uint16
	if federation {
		version, _ := strconv.Atoi(r.Header.Get("X-Wire-API-Version"))
		if single && version < 3 {
			return false
		}
		if r.Method != http.MethodPost {
			w.WriteHeader(405)
			return true
		}
		origin := r.Header.Values("Wire-Origin-Domain")
		if len(origin) == 0 {
			routeParseError(w, "Header Wire-Origin-Domain is required")
			return true
		}
		if _, err := parseEmailDomain(origin[0]); err != nil {
			routeParseError(w, "Error parsing header Wire-Origin-Domain failed: invalid domain")
			return true
		}
		if !publicClientContentType(w, r) {
			return true
		}
		raw, err := publicClientJSON(r)
		if err != nil {
			routeParseError(w, "Invalid MLS client request")
			return true
		}
		fields, err := registrationObject(raw)
		if err != nil {
			routeParseError(w, "Invalid MLS client request")
			return true
		}
		text, err := registrationText(fields, "user_id", true)
		if err != nil {
			routeParseError(w, "Invalid MLS client request")
			return true
		}
		userText = *text
		if !single && version < 1 {
			text, err := registrationText(fields, "signature_scheme", true)
			if err != nil || !validSignatureScheme(*text) {
				routeParseError(w, "Invalid MLS client request")
				return true
			}
			suite = 1
		} else {
			value, err := federationInteger(fields["cipher_suite"])
			if err != nil || value < 0 || value > 65535 {
				routeParseError(w, "Invalid MLS client request")
				return true
			}
			suite = uint16(value)
		}
		if single {
			text, err := registrationText(fields, "client_id", true)
			if err != nil {
				routeParseError(w, "Invalid MLS client request")
				return true
			}
			client = *text
		}
	} else {
		parts := strings.Split(strings.Trim(path, "/"), "/")
		if len(parts) < 4 || parts[0] != "i" || parts[1] != "mls" {
			return false
		}
		single = parts[2] == "client"
		if !single && parts[2] != "clients" || !single && len(parts) != 4 || single && len(parts) != 5 {
			return false
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(405)
			return true
		}
		userText = parts[3]
		if single {
			client = parts[4]
		}
		values, present := r.URL.Query()["ciphersuite"]
		if !present || len(values) == 0 {
			routeParseError(w, "Query parameter ciphersuite is required")
			return true
		}
		var valid bool
		suite, valid = mlsSuiteHex(values[0])
		if !valid {
			routeParseError(w, "Invalid ciphersuite")
			return true
		}
	}
	canonical, valid := tokenUUID(userText)
	if !valid {
		routeParseError(w, "invalid UUID")
		return true
	}
	user, _ := gocql.ParseUUID(canonical)
	if single {
		var err error
		client, err = parseClientID(client)
		if err != nil {
			routeParseError(w, "invalid client ID")
			return true
		}
	}
	if mlsClientScheme(suite) == "" {
		authenticationWireError(w, 400, "mls-protocol-error", "Unknown ciphersuite")
		return true
	}
	if single {
		result, err := s.single(r.Context(), user, client, suite)
		if err != nil {
			authenticationHTTPError(w, err)
		} else {
			readJSON(w, r, result)
		}
	} else {
		result, err := s.list(r.Context(), user, suite)
		if err != nil {
			authenticationHTTPError(w, err)
		} else {
			readJSON(w, r, result)
		}
	}
	return true
}

func (s mlsClientReads) handlePublicCount(w http.ResponseWriter, r *http.Request, path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 5 || parts[0] != "mls" || parts[1] != "key-packages" || parts[2] != "self" || parts[4] != "count" {
		return false
	}
	version, _ := strconv.Atoi(r.Header.Get("X-Wire-API-Version"))
	if version < 5 {
		return false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(405)
		return true
	}
	user, ok := userHeader(w, r)
	if !ok {
		return true
	}
	client, err := parseClientID(parts[3])
	if err != nil {
		routeParseError(w, "invalid client ID")
		return true
	}
	suite := uint16(1)
	values, present := r.URL.Query()["ciphersuite"]
	if (!present || len(values) == 0) && version >= 8 {
		routeParseError(w, "Query parameter ciphersuite is required")
		return true
	}
	if present && len(values) > 0 {
		var ok bool
		suite, ok = mlsSuiteHex(values[0])
		if !ok {
			routeParseError(w, "Invalid ciphersuite")
			return true
		}
	}
	if !s.enabled {
		authenticationWireError(w, 400, "mls-not-enabled", "MLS is not configured on this backend. See docs.wire.com for instructions on how to enable it")
		return true
	}
	if mlsClientScheme(suite) == "" {
		authenticationWireError(w, 400, "mls-protocol-error", "Unknown ciphersuite")
		return true
	}
	count, err := s.count(r.Context(), user, client, suite)
	if err != nil {
		authenticationHTTPError(w, err)
	} else {
		readJSON(w, r, map[string]int64{"count": count})
	}
	return true
}
