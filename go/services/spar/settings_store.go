// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"context"
	"crypto/x509"
	"errors"
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
	root, err := parseXMLNode(blob)
	if err != nil {
		return nil, errors.New("invalid stored IdP certificate")
	}
	return certificateNode(root)
}

func (s cassandraStore) requireIdP(ctx context.Context, id gocql.UUID) error {
	_, err := s.getIdP(ctx, id)
	return err
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
