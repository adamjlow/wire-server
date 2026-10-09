// SPDX-License-Identifier: AGPL-3.0-only
// Storage behavior adapted from uri-bytestring-0.3.3.1.
// Copyright (c) 2014, Soostone Inc; BSD3 notice in third_party/uri-bytestring/LICENSE.
package backgroundworker

import (
	"errors"
	"strconv"
	"strings"
)

// Stored HttpsUrl uses uri-bytestring-0.3.3.1 parse/serialize, not a verbatim
// blob or Go URL.String. In particular it decodes '+' in paths, preserves
// fragment bytes, canonicalizes query pairs and uses uppercase percent escapes.
// No network request or credential handling happens in this storage codec.
func storedHTTPSURL(blob []byte) ([]byte, error) {
	invalid := errors.New("invalid stored HTTPS URL")
	value := string(blob)
	if !strings.HasPrefix(value, "https:") {
		return nil, invalid
	}
	value = value[6:]
	fragment := ""
	if i := strings.IndexByte(value, '#'); i >= 0 {
		fragment = value[i:]
		value = value[:i]
		if !uriCharacters(fragment[1:], "!$&'()*+,;=:@%/?") {
			return nil, invalid
		}
	}
	query := ""
	if i := strings.IndexByte(value, '?'); i >= 0 {
		raw := value[i+1:]
		value = value[:i]
		if !uriCharacters(raw, "!$&'()*+,;=:@%/?") {
			return nil, invalid
		}
		var pairs []string
		for _, pair := range strings.Split(raw, "&") {
			key, val, _ := strings.Cut(pair, "=")
			key = decodeStoredURI(key, true)
			val = decodeStoredURI(val, true)
			if key != "" {
				pairs = append(pairs, encodeStoredURI(key, "")+"="+encodeStoredURI(val, ""))
			}
		}
		if len(pairs) > 0 {
			query = "?" + strings.Join(pairs, "&")
		}
	}
	authority := ""
	if strings.HasPrefix(value, "//") {
		value = value[2:]
		raw := value
		if i := strings.IndexByte(value, '/'); i >= 0 {
			raw = value[:i]
			value = value[i:]
		} else {
			value = ""
		}
		if raw != "" {
			var err error
			authority, err = storedURIAuthority(raw)
			if err != nil {
				return nil, invalid
			}
			authority = "//" + authority
		}
	} else if value != "" {
		return nil, invalid
	}
	if !uriCharacters(value, "!$&'()*+,;=:@%/") {
		return nil, invalid
	}
	path := decodeStoredURI(value, true)
	segments := strings.Split(path, "/")
	for i, segment := range segments {
		segments[i] = encodeStoredURI(segment, ":@&=+$,")
	}
	return []byte("https:" + authority + strings.Join(segments, "/") + query + fragment), nil
}

func uriCharacters(value, extra string) bool {
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("~._-"+extra, c)) {
			return false
		}
	}
	return true
}

func storedURIAuthority(value string) (string, error) {
	invalid := errors.New("invalid stored URI authority")
	userinfo := ""
	if i := strings.IndexByte(value, '@'); i >= 0 {
		raw := value[:i]
		value = value[i+1:]
		if raw == "" || !uriCharacters(raw, "!$&'()*+,;=:%") {
			return "", invalid
		}
		decoded := decodeStoredURI(raw, false)
		user, password, _ := strings.Cut(decoded, ":")
		userinfo = user + ":" + password + "@"
	}
	host, port := value, ""
	if strings.HasPrefix(value, "[") {
		i := strings.IndexByte(value, ']')
		if i < 0 {
			return "", invalid
		}
		host = value[:i+1]
		port = value[i+1:]
		literal := host[1 : len(host)-1]
		if strings.HasPrefix(literal, "v") {
			version, rest, found := strings.Cut(literal[1:], ".")
			if !found || version == "" || rest == "" || !uriHex(version) || !uriCharacters(rest, "!$&'()*+,;=:") {
				return "", invalid
			}
		} else {
			// Native IPv6 parser retains literal spelling and permits omitted
			// groups; it does not enforce Go net.ParseIP's stronger constraint.
			groups := strings.Split(literal, ":")
			count := 0
			for i, group := range groups {
				if group == "" {
					continue
				}
				if strings.Contains(group, ".") {
					if i != len(groups)-1 {
						return "", invalid
					}
					octets := strings.Split(group, ".")
					if len(octets) != 4 {
						return "", invalid
					}
					for _, octet := range octets {
						n, err := strconv.Atoi(octet)
						if err != nil || len(octet) > 3 || n < 1 || n > 255 {
							return "", invalid
						}
					}
					count += 2
				} else {
					if len(group) > 4 || !uriHex(group) {
						return "", invalid
					}
					count++
				}
			}
			if count > 8 {
				return "", invalid
			}
		}
		host = literal // Native ipLiteralParser removes brackets before serialization.
	} else {
		if i := strings.IndexByte(value, ':'); i >= 0 {
			host = value[:i]
			port = value[i:]
		}
		if host == "" || !uriCharacters(host, "!$&'()*+,;=%") {
			return "", invalid
		}
		host = decodeStoredURI(host, false)
	}
	if port != "" {
		if port[0] != ':' || len(port) == 1 {
			return "", invalid
		}
		var n uint64
		for _, c := range port[1:] {
			if c < '0' || c > '9' {
				return "", invalid
			}
			n = n*10 + uint64(c-'0')
		}
		// Attoparsec decimal into the source's 64-bit Int wraps on overflow.
		port = ":" + strconv.FormatInt(int64(n), 10)
	}
	return userinfo + host + port, nil
}

func uriHex(value string) bool {
	for _, c := range value {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

func decodeStoredURI(value string, plusToSpace bool) string {
	var result strings.Builder
	for i := 0; i < len(value); i++ {
		if plusToSpace && value[i] == '+' {
			result.WriteByte(' ')
			continue
		}
		if value[i] == '%' && i+2 < len(value) && uriHex(value[i+1:i+3]) {
			n, _ := strconv.ParseUint(value[i+1:i+3], 16, 8)
			result.WriteByte(byte(n))
			i += 2
			continue
		}
		// Native malformed percent sequences survive decoding and are then
		// percent-encoded on path/query serialization, not silently dropped.
		result.WriteByte(value[i])
	}
	return result.String()
}

func encodeStoredURI(value, extra string) string {
	const hex = "0123456789ABCDEF"
	var result strings.Builder
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("~._-"+extra, rune(c)) {
			result.WriteByte(c)
		} else {
			result.WriteByte('%')
			result.WriteByte(hex[c>>4])
			result.WriteByte(hex[c&15])
		}
	}
	return result.String()
}
