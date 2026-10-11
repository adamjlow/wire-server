// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/base64"
	"errors"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type mlsClientInfo struct {
	ID           string  `json:"id"`
	SignatureKey *string `json:"mls_signature_key,omitempty"`
	MLS          bool    `json:"mls"`
}
type mlsPackageRow struct{ ref, data []byte }
type mlsClientReads struct {
	ids      func(context.Context, gocql.UUID) ([]string, error)
	get      func(context.Context, gocql.UUID, string) (*storedClient, error)
	packages func(context.Context, gocql.UUID, string, uint16) ([]mlsPackageRow, error)
	remove   func(context.Context, gocql.UUID, string, uint16, [][]byte) error
	now      func() time.Time
	maximum  *mlsLifetimeMaximum
	enabled  bool
}

func mlsClientScheme(suite uint16) string {
	switch suite {
	case 1:
		return "ed25519"
	case 2:
		return "ecdsa_secp256r1_sha256"
	case 7:
		return "ecdsa_secp384r1_sha384"
	case 5:
		return "ecdsa_secp521r1_sha512"
	}
	return ""
}
func (s clientStore) mlsPackages(ctx context.Context, user gocql.UUID, client string, suite uint16) ([]mlsPackageRow, error) {
	iter := s.cassandra.Query(`SELECT ref, data FROM mls_key_packages WHERE user = ? AND client = ? AND cipher_suite = ?`, user, client, int32(suite)).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).Iter()
	rows := []mlsPackageRow{}
	var ref, data *[]byte
	for iter.Scan(&ref, &data) {
		if ref == nil || data == nil {
			_ = iter.Close()
			return nil, errors.New("invalid MLS package row")
		}
		rows = append(rows, mlsPackageRow{ref: append([]byte{}, (*ref)...), data: append([]byte{}, (*data)...)})
	}
	if err := iter.Close(); err != nil {
		return nil, errors.New("MLS package lookup failed")
	}
	return rows, nil
}
func (s clientStore) deleteMLSPackages(ctx context.Context, user gocql.UUID, client string, suite uint16, refs [][]byte) error {
	// Native executes this query even with an empty reference list.
	return s.cassandra.Query(`DELETE FROM mls_key_packages WHERE user = ? AND client = ? AND cipher_suite = ? AND ref IN ?`, user, client, int32(suite), refs).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5}).Exec()
}
func (s mlsClientReads) count(ctx context.Context, user gocql.UUID, client string, suite uint16) (int64, error) {
	var result int64
	rows, err := s.packages(ctx, user, client, suite)
	if err != nil {
		return result, err
	}
	now := s.now()
	expired := [][]byte{}
	for _, row := range rows {
		lifetime, decoded := decodeMLSPackageLifetime(row.data)
		if !decoded {
			continue
		} // malformed rows are retained, not counted
		if lifetime.usable(now, s.maximum) {
			result++
		} else {
			expired = append(expired, row.ref)
		}
	}
	if err := s.remove(ctx, user, client, suite, expired); err != nil {
		return result, err
	}
	return result, nil
}
func (s mlsClientReads) single(ctx context.Context, user gocql.UUID, client string, suite uint16) (mlsClientInfo, error) {
	result := mlsClientInfo{ID: client}
	count, err := s.count(ctx, user, client, suite)
	if err != nil {
		return result, err
	}
	result.MLS = count > 0
	stored, err := s.get(ctx, user, client)
	if err != nil {
		return result, err
	}
	if stored != nil {
		if key, ok := stored.PublicKeys[mlsClientScheme(suite)]; ok {
			encoded := base64.StdEncoding.EncodeToString(key)
			result.SignatureKey = &encoded
		}
	}
	return result, nil
}
func (s mlsClientReads) list(ctx context.Context, user gocql.UUID, suite uint16) ([]mlsClientInfo, error) {
	ids, err := s.ids(ctx, user)
	if err != nil {
		return nil, err
	}
	ids, err = nativeClientIDSet(ids)
	if err != nil {
		return nil, err
	}
	result := []mlsClientInfo{}
	for _, id := range ids {
		client, err := s.single(ctx, user, id, suite)
		if err != nil {
			return nil, err
		}
		result = append(result, client)
	}
	return result, nil
}
