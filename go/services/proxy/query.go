// SPDX-License-Identifier: AGPL-3.0-or-later
package proxy

import (
	"net/url"
	"strings"
)

type queryItem struct {
	key   string
	value *string
}

// Keep pairs ordered, including duplicate keys, bare keys and empty values.
// url.Values would sort keys and lose distinctions present in WAI's Query.
func parseQuery(raw string) []queryItem {
	var result []queryItem
	for _, pair := range strings.Split(raw, "&") {
		if pair == "" {
			continue
		}
		key, value, present := strings.Cut(pair, "=")
		item := queryItem{key: decodeQueryBytes(key)}
		if present {
			decoded := decodeQueryBytes(value)
			item.value = &decoded
		}
		result = append(result, item)
	}
	return result
}

// HTTP-types decodes valid percent pairs while retaining malformed '%' bytes.
// This is a byte operation: opaque non-UTF8 provider query values stay opaque.
func decodeQueryBytes(value string) string {
	var out strings.Builder
	for i := 0; i < len(value); i++ {
		switch value[i] {
		case '+':
			out.WriteByte(' ')
		case '%':
			if i+2 < len(value) {
				decoded, err := url.QueryUnescape(value[i : i+3])
				if err == nil {
					out.WriteString(decoded)
					i += 2
					continue
				}
			}
			out.WriteByte('%')
		default:
			out.WriteByte(value[i])
		}
	}
	return out.String()
}

func encodeQueryBytes(value string) string {
	return strings.ReplaceAll(url.QueryEscape(value), "+", "%20")
}

func renderQuery(items []queryItem) string {
	var pairs []string
	for _, item := range items {
		pair := encodeQueryBytes(item.key)
		if item.value != nil {
			pair += "=" + encodeQueryBytes(*item.value)
		}
		pairs = append(pairs, pair)
	}
	return strings.Join(pairs, "&")
}

func providerQuery(raw, credentialKey, credential string) string {
	items := []queryItem{{key: credentialKey, value: &credential}}
	for _, item := range parseQuery(raw) {
		// ByteString's case fold does not equate UTF-8 Unicode lookalikes with
		// ASCII letters (for example Kelvin sign with K).
		if len(item.key) != len("access_token") || !strings.EqualFold(item.key, "access_token") {
			items = append(items, item)
		}
	}
	return renderQuery(items)
}
