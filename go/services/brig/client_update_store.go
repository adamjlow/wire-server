// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"
	"net/http"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type clientUpdateStore interface {
	get(context.Context, gocql.UUID, string) (*storedClient, error)
	updateLabel(context.Context, gocql.UUID, string, string) error
	updateCapabilities(context.Context, gocql.UUID, string, []int32) error
	updatePrekeys(context.Context, gocql.UUID, string, []uncheckedPrekey) error
	addMLSPublicKeys(context.Context, gocql.UUID, string, map[string][]byte) error
}

func (s clientStore) mutation(ctx context.Context, statement string, args ...any) error {
	return userStorageError(s.cassandra.Query(statement, args...).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5}).Exec())
}
func (s clientStore) updateLabel(ctx context.Context, user gocql.UUID, id, label string) error {
	return s.mutation(ctx, `UPDATE clients SET label=? WHERE user=? AND client=?`, label, user, id)
}
func (s clientStore) updateCapabilities(ctx context.Context, user gocql.UUID, id string, caps []int32) error {
	return s.mutation(ctx, `UPDATE clients SET capabilities=? WHERE user=? AND client=?`, caps, user, id)
}
func (s clientStore) updatePrekeys(ctx context.Context, user gocql.UUID, id string, keys []uncheckedPrekey) error {
	for _, key := range keys {
		if err := s.mutation(ctx, `INSERT INTO prekeys (user,client,key,data) VALUES (?,?,?,?)`, user, id, int32(key.ID), key.Key); err != nil {
			return err
		}
	}
	return nil
}

var nativeSignatureSchemes = []string{"ed25519", "ecdsa_secp256r1_sha256", "ecdsa_secp384r1_sha384", "ecdsa_secp521r1_sha512"}

func (s clientStore) addMLSPublicKeys(ctx context.Context, user gocql.UUID, id string, keys map[string][]byte) error {
	// Native Map.assocs follows the SignatureSchemeTag enum, not text order.
	for scheme := range keys {
		if !validSignatureScheme(scheme) {
			return errors.New("invalid MLS signature scheme")
		}
	}
	for _, scheme := range nativeSignatureSchemes {
		key, present := keys[scheme]
		if !present {
			continue
		}
		row := map[string]any{}
		applied, err := s.cassandra.Query(`INSERT INTO mls_public_keys (user,client,sig_scheme,key) VALUES (?,?,?,?) IF NOT EXISTS`, user, id, scheme, key).WithContext(ctx).Consistency(gocql.LocalQuorum).SerialConsistency(gocql.LocalSerial).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 0}).MapScanCAS(row)
		if err != nil {
			return userStorageError(err)
		}
		if !applied {
			return errClientMLSDuplicate
		}
	}
	return nil
}

func (p brigPeers) setupConsumableClient(ctx context.Context, user gocql.UUID, id, requestID string) error {
	_, _, err := p.replayableRPCStatus(ctx, p.gundeck, http.MethodPost, "/i/users/"+user.String()+"/clients/"+id+"/consumable-notifications", requestID, nil, false, false, nil)
	if err != nil {
		return errors.New("consumable notification client setup failed")
	}
	return nil
}
