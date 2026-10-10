// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"context"
	"encoding/xml"
	"errors"
	"net/mail"
	"strings"
	"unicode/utf8"

	"github.com/apache/cassandra-gocql-driver/v2"
	"golang.org/x/text/cases"
)

const assertionNS = "urn:oasis:names:tc:SAML:2.0:assertion"
const nameID11 = "urn:oasis:names:tc:SAML:1.1:nameid-format:"
const nameID20 = "urn:oasis:names:tc:SAML:2.0:nameid-format:"
const nativeNamespaces = ` xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" xmlns:samla="urn:oasis:names:tc:SAML:2.0:assertion" xmlns:samlm="urn:oasis:names:tc:SAML:2.0:metadata" xmlns:ds="http://www.w3.org/2000/09/xmldsig#"`

type samlNameID struct {
	Format, Value                                string
	NameQualifier, SPNameQualifier, SPProvidedID *string
}

func optionalXMLAttr(n *xmlNode, name string) *string {
	for _, attr := range n.Attributes {
		if attr.Name.Space == "" && attr.Name.Local == name {
			value := attr.Value
			return &value
		}
	}
	return nil
}

func parseNameID(encoded string) (samlNameID, error) {
	var id samlNameID
	n, err := parseXMLNode([]byte(encoded))
	bad := errors.New("invalid SAML NameID")
	if err != nil || n.Name != (xml.Name{Space: assertionNS, Local: "NameID"}) || len(n.Children) != 0 {
		return id, bad
	}
	id = samlNameID{Format: n.attr("Format"), Value: n.Text, NameQualifier: optionalXMLAttr(n, "NameQualifier"), SPNameQualifier: optionalXMLAttr(n, "SPNameQualifier"), SPProvidedID: optionalXMLAttr(n, "SPProvidedID")}
	if id.Format == "" {
		id.Format = nameID11 + "unspecified"
	}
	switch id.Format {
	case nameID11 + "unspecified", nameID11 + "X509SubjectName", nameID11 + "WindowsDomainQualifiedName", nameID20 + "kerberos", nameID20 + "transient":
	case nameID11 + "emailAddress":
		// Preserve original casing; case folding applies only to the lookup key.
		address, err := mail.ParseAddress(id.Value)
		if err != nil || address.Name != "" || address.Address != id.Value {
			return samlNameID{}, bad
		}
	case nameID20 + "persistent":
		if utf8.RuneCountInString(id.Value) > 1024 {
			return samlNameID{}, bad
		}
	case nameID20 + "entity":
		if !absoluteURI(id.Value) || utf8.RuneCountInString(id.Value) > 1024 {
			return samlNameID{}, bad
		}
		// Native mkNameID rejects the all-three combination then discards
		// qualifiers for entity IDs. Preserve that observable source behavior.
		if id.NameQualifier != nil && id.SPNameQualifier != nil && id.SPProvidedID != nil {
			return samlNameID{}, bad
		}
		id.NameQualifier, id.SPNameQualifier, id.SPProvidedID = nil, nil, nil
	default:
		return samlNameID{}, bad
	}
	return id, nil
}

func (id samlNameID) normalized() string { return cases.Fold().String(id.Value) }

// Match Text.XML's element serializer: declaration-free default assertion
// namespace, fixed namespace declarations, ordered attributes and contextual
// escaping. This byte string is also a legacy Cassandra clustering key.
func nativeXMLEscape(value string, attribute bool) string {
	if attribute {
		return strings.NewReplacer("&", "&amp;", "<", "&lt;", `"`, "&quot;").Replace(value)
	}
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(value)
}

func (id samlNameID) encoded() string {
	var b strings.Builder
	b.WriteString("<NameID" + nativeNamespaces)
	for _, attr := range []struct {
		name  string
		value *string
	}{{"Format", &id.Format}, {"NameQualifier", id.NameQualifier}, {"SPNameQualifier", id.SPNameQualifier}, {"SPProvidedID", id.SPProvidedID}} {
		if attr.value != nil {
			b.WriteString(" " + attr.name + `="` + nativeXMLEscape(*attr.value, true) + `"`)
		}
	}
	b.WriteString(` xmlns="` + assertionNS + `"`)
	if id.Value == "" {
		b.WriteString("/>")
	} else {
		b.WriteString(">" + nativeXMLEscape(id.Value, false) + "</NameID>")
	}
	return b.String()
}

type samlUserRef struct {
	Issuer  string
	Subject samlNameID
}

func (s cassandraStore) insertSAMLUser(ctx context.Context, ref samlUserRef, uid gocql.UUID) error {
	return s.write(ctx, "INSERT INTO user_v2 (issuer, normalized_uname_id, sso_id, uid) VALUES (?, ?, ?, ?)", ref.Issuer, ref.Subject.normalized(), ref.Subject.encoded(), uid)
}

func (s cassandraStore) samlUser(ctx context.Context, ref samlUserRef) (*gocql.UUID, error) {
	var uid *gocql.UUID
	err := s.read(ctx, "SELECT uid FROM user_v2 WHERE issuer = ? AND normalized_uname_id = ?", ref.Issuer, ref.Subject.normalized()).Scan(&uid)
	if err == nil && uid == nil {
		return nil, errors.New("invalid stored SAML user")
	}
	if !errors.Is(err, gocql.ErrNotFound) {
		return uid, err
	}
	err = s.read(ctx, "SELECT uid FROM user WHERE issuer = ? AND sso_id = ?", ref.Issuer, ref.Subject.encoded()).Scan(&uid)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if uid == nil {
		return nil, errors.New("invalid stored legacy SAML user")
	}
	// The source copies on lookup and leaves the legacy row intact.
	return uid, s.insertSAMLUser(ctx, ref, *uid)
}

func (s cassandraStore) deleteSAMLUser(ctx context.Context, uid gocql.UUID, ref samlUserRef) error {
	stored, err := s.samlUser(ctx, ref)
	if err != nil || stored == nil || *stored != uid {
		return err
	}
	if err = s.write(ctx, "DELETE FROM user WHERE issuer = ? AND sso_id = ?", ref.Issuer, ref.Subject.encoded()); err != nil {
		return err
	}
	return s.write(ctx, "DELETE FROM user_v2 WHERE issuer = ? AND normalized_uname_id = ?", ref.Issuer, ref.Subject.normalized())
}

func (s cassandraStore) deleteSAMLUsersByIssuer(ctx context.Context, issuer string) error {
	return s.write(ctx, "DELETE FROM user_v2 WHERE issuer = ?", issuer)
}

type mappedSAMLUser struct {
	Ref samlUserRef
	UID gocql.UUID
}

func (s cassandraStore) samlUsersPage(ctx context.Context, issuer string, state []byte) ([]mappedSAMLUser, []byte, error) {
	// Setting PageState (including nil) disables driver auto-paging, retaining
	// native page boundaries before the account lookups/deletes for each page.
	iter := s.read(ctx, "SELECT sso_id, uid FROM user_v2 WHERE issuer = ?", issuer).PageSize(201).PageState(state).Iter()
	users := []mappedSAMLUser{}
	var encoded *string
	var uid *gocql.UUID
	for iter.Scan(&encoded, &uid) {
		if encoded == nil || uid == nil {
			_ = iter.Close()
			return nil, nil, errors.New("invalid stored SAML user")
		}
		subject, err := parseNameID(*encoded)
		if err != nil {
			_ = iter.Close()
			return nil, nil, err
		}
		users = append(users, mappedSAMLUser{samlUserRef{issuer, subject}, *uid})
	}
	next := append([]byte(nil), iter.PageState()...)
	return users, next, iter.Close()
}
