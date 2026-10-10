// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"strings"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type cassandraStore struct{ session *gocql.Session }

func (s cassandraStore) read(ctx context.Context, query string, values ...any) *gocql.Query {
	return s.session.Query(query, values...).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1})
}
func (s cassandraStore) write(ctx context.Context, query string, values ...any) error {
	return s.session.Query(query, values...).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5}).Exec()
}
func (s cassandraStore) defaultSSO(ctx context.Context) (*gocql.UUID, error) {
	var id gocql.UUID
	err := s.read(ctx, "SELECT idp FROM default_idp WHERE partition_key_always_default = 'default' ORDER BY idp LIMIT 1").Scan(&id)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, nil
	}
	return &id, err
}

// Native CQL certificates are UTF-8 XML KeyInfo blobs, not raw DER. Certificate
// validity dates are deliberately not web-PKI policy for stored signing keys.
func decodeStoredCertificate(blob []byte) (*x509.Certificate, error) {
	var key struct {
		XMLName xml.Name `xml:"http://www.w3.org/2000/09/xmldsig# KeyInfo"`
		Data    struct {
			Certificates []string `xml:"http://www.w3.org/2000/09/xmldsig# X509Certificate"`
		} `xml:"http://www.w3.org/2000/09/xmldsig# X509Data"`
	}
	if xml.Unmarshal(blob, &key) != nil || len(key.Data.Certificates) != 1 {
		return nil, errors.New("invalid stored IdP certificate")
	}
	encoded := strings.Join(strings.Fields(key.Data.Certificates[0]), "")
	der, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, errors.New("invalid stored IdP certificate")
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, errors.New("invalid stored IdP certificate")
	}
	return cert, nil
}

func (s cassandraStore) requireIdP(ctx context.Context, id gocql.UUID) error {
	// Read the complete typed row, as the reference does before accepting the
	// default. Missing or malformed rows must never authorize a setting change.
	var stored, team *gocql.UUID
	var issuer, uri string
	var head []byte
	var tail [][]byte
	var version *int32
	var oldIssuers []string
	var replaced *gocql.UUID
	var handle, domain *string
	err := s.read(ctx, "SELECT idp, issuer, request_uri, public_key, extra_public_keys, team, api_version, old_issuers, replaced_by, handle, domain FROM idp WHERE idp = ?", id).Scan(&stored, &issuer, &uri, &head, &tail, &team, &version, &oldIssuers, &replaced, &handle, &domain)
	if errors.Is(err, gocql.ErrNotFound) {
		return errIdPNotFound
	}
	if err != nil {
		return err
	}
	if stored == nil || team == nil {
		return errors.New("invalid stored IdP row")
	}
	if version != nil && *version != 1 && *version != 2 {
		return errors.New("invalid stored IdP API version")
	}
	for _, oldIssuer := range oldIssuers {
		if !absoluteURI(oldIssuer) {
			return errors.New("invalid stored IdP issuer")
		}
	}
	if !absoluteURI(issuer) || !absoluteURI(uri) {
		return errors.New("invalid stored IdP URI")
	}
	for _, cert := range append([][]byte{head}, tail...) {
		if _, err = decodeStoredCertificate(cert); err != nil {
			return err
		}
	}
	return nil
}
func (s cassandraStore) setDefaultSSO(ctx context.Context, id *gocql.UUID) error {
	if id != nil {
		if err := s.requireIdP(ctx, *id); err != nil {
			return err
		}
	}
	if err := s.write(ctx, "DELETE FROM default_idp WHERE partition_key_always_default = 'default'"); err != nil {
		return err
	}
	if id == nil {
		return nil
	}
	return s.write(ctx, "INSERT INTO default_idp (partition_key_always_default, idp) VALUES ('default', ?)", *id)
}
func (s cassandraStore) userCreated(ctx context.Context, id gocql.UUID) (*time.Time, error) {
	var created, updated *time.Time
	var emailType *string
	var emailPrimary *bool
	err := s.read(ctx, "SELECT created_at, last_updated_at, email_type, email_primary FROM scim_user_times WHERE uid = ?", id).Scan(&created, &updated, &emailType, &emailPrimary)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, nil
	}
	if err == nil && (created == nil || updated == nil) {
		return nil, errors.New("invalid stored SCIM metadata")
	}
	return created, err
}
