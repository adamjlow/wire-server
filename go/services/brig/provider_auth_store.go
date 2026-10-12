// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func (s providerAccountStore) byEmail(ctx context.Context, email string) (*gocql.UUID, error) {
	key, err := emailUniqueKey(email)
	if err != nil {
		return nil, errors.New("invalid provider email")
	}
	var id gocql.UUID
	err = s.cassandra.Query(`SELECT provider FROM provider_keys WHERE key=?`, key).
		WithContext(ctx).Consistency(gocql.LocalQuorum).
		RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).Scan(&id)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, userStorageError(err)
	}
	return &id, nil
}

func (s providerAccountStore) password(ctx context.Context, id gocql.UUID) (*string, error) {
	var encoded []byte
	err := s.cassandra.Query(`SELECT password FROM provider WHERE id=?`, id).
		WithContext(ctx).Consistency(gocql.LocalQuorum).
		RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).Scan(&encoded)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, userStorageError(err)
	}
	if encoded == nil {
		return nil, nil
	}
	value := string(encoded)
	return &value, nil
}

func (s providerAccountStore) setPassword(ctx context.Context, id gocql.UUID, encoded string) error {
	return userStorageError(s.cassandra.Query(`UPDATE provider SET password=? WHERE id=?`, []byte(encoded), id).
		WithContext(ctx).Consistency(gocql.LocalQuorum).
		RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5}).Exec())
}
