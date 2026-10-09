// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"strings"
	"unicode/utf8"
)

const samlAssertionNamespace = "urn:oasis:names:tc:SAML:2.0:assertion"

const samlStorageNamespaces = ` xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" xmlns:samla="urn:oasis:names:tc:SAML:2.0:assertion" xmlns:samlm="urn:oasis:names:tc:SAML:2.0:metadata" xmlns:ds="http://www.w3.org/2000/09/xmldsig#"`

var samlNameFormats = map[string]string{
	"unspecified":                "urn:oasis:names:tc:SAML:1.1:nameid-format:unspecified",
	"emailAddress":               "urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress",
	"X509SubjectName":            "urn:oasis:names:tc:SAML:1.1:nameid-format:X509SubjectName",
	"WindowsDomainQualifiedName": "urn:oasis:names:tc:SAML:1.1:nameid-format:WindowsDomainQualifiedName",
	"kerberos":                   "urn:oasis:names:tc:SAML:2.0:nameid-format:kerberos",
	"entity":                     "urn:oasis:names:tc:SAML:2.0:nameid-format:entity",
	"persistent":                 "urn:oasis:names:tc:SAML:2.0:nameid-format:persistent",
	"transient":                  "urn:oasis:names:tc:SAML:2.0:nameid-format:transient",
}

type storedSAMLName struct {
	value, format string
	qualifiers    map[string]string
}

// Native Cql UserSSOId is JSON in text, not a blob. Re-encode its parsed value
// as PG JSON: legacy plaintext and noncanonical XML cannot be copied verbatim.
func decodeUserMigrationSSO(value any) (json.RawMessage, error) {
	if value == nil {
		return nil, nil
	}
	invalid := errors.New("invalid stored user SSO identity")
	text, err := userMigrationString(value)
	if err != nil {
		return nil, invalid
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(text), &fields) != nil || fields == nil {
		return nil, invalid
	}
	optional := func(key string) (*string, error) {
		raw := fields[key]
		if len(raw) == 0 || string(raw) == "null" {
			return nil, nil
		}
		var v string
		if json.Unmarshal(raw, &v) != nil {
			return nil, invalid
		}
		return &v, nil
	}
	tenant, err := optional("tenant")
	if err != nil {
		return nil, err
	}
	subject, err := optional("subject")
	if err != nil {
		return nil, err
	}
	scim, err := optional("scim_external_id")
	if err != nil {
		return nil, err
	}
	if tenant == nil && subject == nil && scim != nil {
		return json.Marshal(map[string]string{"scim_external_id": *scim})
	}
	if tenant == nil || subject == nil || scim != nil {
		return nil, invalid
	}
	issuer, err := storedSAMLIssuer(*tenant)
	if err != nil {
		return nil, invalid
	}
	name, err := storedSAMLSubject(*subject)
	if err != nil {
		return nil, invalid
	}
	return json.Marshal(map[string]string{"tenant": issuer, "subject": name})
}

func storedSAMLIssuer(value string) (string, error) {
	name, err := parseStoredSAMLName(value, "Issuer")
	if err == nil && name.format == "entity" {
		return renderStoredSAMLName(name, "Issuer"), nil
	}
	// Plain tenant fallback does not impose mkNameID's XML entity URI limit
	// or strip its text before parsing, unlike parseURI' on XML entity content.
	uri, err := storedAbsoluteURI([]byte(value), true, true)
	if err != nil {
		return "", err
	}
	return renderStoredSAMLName(storedSAMLName{value: string(uri), format: "entity"}, "Issuer"), nil
}

func storedSAMLSubject(value string) (string, error) {
	name, err := parseStoredSAMLName(value, "NameID")
	if err == nil {
		return renderStoredSAMLName(name, "NameID"), nil
	}
	if email, err := canonicalUserMigrationEmail(value); err == nil {
		return renderStoredSAMLName(storedSAMLName{value: email, format: "emailAddress"}, "NameID"), nil
	}
	// The legacy plaintext fallback uses API mkName (1..128 characters).
	// Even malformed XML can be plain Name text; safely escape it on output.
	if n := utf8.RuneCountInString(value); n < 1 || n > 128 {
		return "", errors.New("invalid stored SAML subject")
	}
	return renderStoredSAMLName(storedSAMLName{value: value, format: "unspecified"}, "NameID"), nil
}

func parseStoredSAMLName(value, element string) (storedSAMLName, error) {
	invalid := errors.New("invalid stored SAML XML")
	name := storedSAMLName{format: "unspecified", qualifiers: make(map[string]string)}
	if element == "Issuer" {
		name.format = "entity"
	}
	decoder := xml.NewDecoder(strings.NewReader(value))
	started, ended := false, false
	var content strings.Builder
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return name, invalid
		}
		switch token := token.(type) {
		case xml.StartElement:
			if started || token.Name.Local != element || token.Name.Space != samlAssertionNamespace {
				return name, invalid
			}
			started = true
			seen := make(map[xml.Name]bool)
			for _, attr := range token.Attr {
				if attr.Name.Space == "xmlns" || attr.Name.Space == "" && attr.Name.Local == "xmlns" {
					continue
				}
				if attr.Name.Space != "" || seen[attr.Name] {
					return name, invalid
				}
				seen[attr.Name] = true
				switch attr.Name.Local {
				case "Format":
					name.format = ""
					for format, uri := range samlNameFormats {
						if attr.Value == uri {
							name.format = format
						}
					}
					if name.format == "" {
						return name, invalid
					}
				case "NameQualifier", "SPNameQualifier", "SPProvidedID":
					name.qualifiers[attr.Name.Local] = attr.Value
				default:
					return name, invalid
				}
			}
		case xml.EndElement:
			ended = true
		case xml.CharData:
			if started && !ended {
				content.Write(token)
			} else if strings.TrimSpace(string(token)) != "" {
				return name, invalid
			}
		case xml.Directive:
			// No DTD or entity resolution, file access, or outbound requests.
			return name, invalid
		}
	}
	if !started || !ended {
		return name, invalid
	}
	name.value = content.String()
	switch name.format {
	case "emailAddress":
		email, err := canonicalUserMigrationEmail(name.value)
		if err != nil {
			return name, invalid
		}
		name.value = email
	case "entity":
		// Native mkNameID rejects only when all three qualifiers are present;
		// otherwise it discards every qualifier. Preserve this source quirk.
		if len(name.qualifiers) == 3 {
			return name, invalid
		}
		uri, err := storedAbsoluteURI([]byte(strings.TrimSpace(name.value)), true, true)
		if err != nil || utf8.RuneCount(uri) > 1024 {
			return name, invalid
		}
		name.value, name.qualifiers = string(uri), nil
	case "persistent":
		if utf8.RuneCountInString(name.value) > 1024 {
			return name, invalid
		}
	}
	return name, nil
}

func renderStoredSAMLName(name storedSAMLName, element string) string {
	var result strings.Builder
	result.WriteString("<" + element + samlStorageNamespaces)
	if element == "NameID" && name.format != "unspecified" {
		result.WriteString(` Format="` + samlNameFormats[name.format] + `"`)
	}
	for _, key := range []string{"NameQualifier", "SPNameQualifier", "SPProvidedID"} {
		if value, present := name.qualifiers[key]; present {
			result.WriteString(" " + key + `="` + escapeStoredSAML(value, true) + `"`)
		}
	}
	result.WriteString(` xmlns="` + samlAssertionNamespace + `"`)
	if name.value == "" {
		result.WriteString("/>")
	} else {
		result.WriteString(">" + escapeStoredSAML(name.value, false) + "</" + element + ">")
	}
	return result.String()
}

func escapeStoredSAML(value string, attribute bool) string {
	// Text.XML's renderer escapes quotes only in attributes, '>' only in
	// content, and preserves whitespace rather than Go EscapeText's numerics.
	value = strings.ReplaceAll(value, "&", "&amp;")
	value = strings.ReplaceAll(value, "<", "&lt;")
	if attribute {
		return strings.ReplaceAll(value, `"`, "&quot;")
	}
	return strings.ReplaceAll(value, ">", "&gt;")
}
