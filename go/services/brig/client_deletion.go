// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"

	"github.com/apache/cassandra-gocql-driver/v2"
)

var errClientLegalHoldDeletion = errors.New("legalhold client cannot be removed")

type clientDeletionService struct {
	lookup         func(context.Context, gocql.UUID, string) (*storedClient, error)
	reauthenticate func(context.Context, gocql.UUID, *string) error
	revokeLabel    func(context.Context, gocql.UUID, string) error
	enqueue        func(context.Context, gocql.UUID, string, *string) error
	remove         func(context.Context, gocql.UUID, string) error
}

func (s clientDeletionService) removePublic(ctx context.Context, user gocql.UUID, id, connection string, password *string) error {
	client, err := s.lookup(ctx, user, id)
	if err != nil {
		return err
	}
	if client == nil {
		return errClientMissing
	}
	switch client.Type {
	case 2:
		return errClientLegalHoldDeletion
	case 0: // Native temporary clients skip reauthentication.
	default:
		if err := s.reauthenticate(ctx, user, password); err != nil {
			return err
		}
	}
	return s.execute(ctx, user, &connection, *client)
}

// Native execDelete is deliberately not transactional. Queue failure leaves
// cookie revocation committed; storage failure leaves the event published.
func (s clientDeletionService) execute(ctx context.Context, user gocql.UUID, connection *string, client storedClient) error {
	if client.Cookie != nil {
		if err := s.revokeLabel(ctx, user, *client.Cookie); err != nil {
			return err
		}
	}
	if err := s.enqueue(ctx, user, client.ID, connection); err != nil {
		return err
	}
	return s.remove(ctx, user, client.ID)
}

type clientDeletionStorage struct {
	clientStore
	deleteLock func(context.Context, gocql.UUID, string) error
}

func (s clientDeletionStorage) remove(ctx context.Context, user gocql.UUID, id string) error {
	if err := s.mutation(ctx, `DELETE FROM clients WHERE user=? AND client=?`, user, id); err != nil {
		return err
	}
	if err := s.mutation(ctx, `DELETE FROM prekeys WHERE user=? AND client=?`, user, id); err != nil {
		return err
	}
	// Native retains MLS public-key rows. Optimistic Dynamo lock deletion follows
	// both CQL writes, whereas random-prekey/local-lock mode has no lock row.
	return s.deleteLock(ctx, user, id)
}
