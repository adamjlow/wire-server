// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func (s providerAccountStore) create(ctx context.Context, request providerRegistrationRequest, encoded string) (gocql.UUID, error) {
	id, err := gocql.RandomUUID()
	if err != nil {
		return gocql.UUID{}, userStorageError(err)
	}
	err = s.cassandra.Query(`INSERT INTO provider (id,name,password,url,descr) VALUES (?,?,?,?,?)`, id, request.name, []byte(encoded), []byte(request.url), request.description).
		WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5}).Exec()
	return id, userStorageError(err)
}

func (s providerAccountStore) activate(ctx context.Context, id gocql.UUID, old *string, email string) error {
	key, err := emailUniqueKey(email)
	if err != nil {
		return err
	}
	batch := s.cassandra.NewBatch(gocql.LoggedBatch).WithContext(ctx)
	batch.SetConsistency(gocql.LocalQuorum)
	batch.RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5})
	if old != nil {
		oldKey, err := emailUniqueKey(*old)
		if err != nil {
			return err
		}
		batch.Query(`DELETE FROM provider_keys WHERE key=?`, oldKey)
	}
	batch.Query(`INSERT INTO provider_keys (key,provider) VALUES (?,?)`, key, id)
	batch.Query(`UPDATE provider SET email=? WHERE id=?`, email, id)
	return userStorageError(s.cassandra.ExecuteBatch(batch))
}
