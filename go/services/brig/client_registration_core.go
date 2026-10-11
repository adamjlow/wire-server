// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

var errClientCapacity = errors.New("too many clients")

// This is the native upsertClient boundary. Account/code admission and the
// subsequent peer, deletion, event and email effects belong to addClient.
type clientRegistrationStore interface {
	read(context.Context, gocql.UUID, *string) ([]storedClient, error)
	insertClient(context.Context, gocql.UUID, storedClient, []uncheckedPrekey) error
}

type clientRegistrationRequest struct {
	client           storedClient
	last             uncheckedPrekey
	prekeys          []uncheckedPrekey
	password         *string
	verificationCode *string
}

func (clientRegistrationRequest) String() string { return "<ClientRegistrationRequest>" }

type clientRegistrationCore struct {
	store          clientRegistrationStore
	maxPermanent   int
	now            func() time.Time
	reauthenticate func(context.Context, gocql.UUID, *string) error
	// Native internal handlers may supply AddClientWithPolicy. Nil selects
	// reAuthForNewClients, the public/default policy.
	reauthPolicy func(int, bool) bool
}

func (s clientRegistrationCore) upsert(ctx context.Context, user gocql.UUID, request clientRegistrationRequest) (storedClient, []storedClient, int, error) {
	var zero storedClient
	id := clientIDFromPrekey(request.last)
	clients, err := s.store.read(ctx, user, nil)
	if err != nil {
		return zero, nil, 0, err
	}
	typed := []storedClient{}
	exists := false
	for _, client := range clients {
		if client.Type == request.client.Type {
			typed = append(typed, client)
			exists = exists || client.ID == id
		}
	}
	needsAuth := len(typed) > 0 && !exists
	if s.reauthPolicy != nil {
		needsAuth = s.reauthPolicy(len(typed), exists)
	}
	if needsAuth {
		if s.reauthenticate == nil {
			return zero, nil, 0, errors.New("client reauthentication unavailable")
		}
		if err := s.reauthenticate(ctx, user, request.password); err != nil {
			return zero, nil, 0, err
		}
	}
	if request.client.Type == 1 && len(typed) >= s.maxPermanent && !exists {
		return zero, nil, 0, errClientCapacity
	}
	// Native reads time before validating every key, then writes the last key first.
	created := s.now().UTC().Truncate(time.Millisecond)
	keys := append([]uncheckedPrekey{request.last}, request.prekeys...)
	for _, key := range keys {
		if !checkPrekeyBundle(key) {
			return zero, nil, 0, errPrekeyBundle
		}
	}
	client := request.client
	client.ID, client.Created, client.LastActive = id, created, nil
	if err := s.store.insertClient(ctx, user, client, keys); err != nil {
		return zero, nil, 0, err
	}
	old := []storedClient{}
	if client.Type != 1 {
		for _, candidate := range typed {
			if candidate.ID != id {
				old = append(old, candidate)
			}
		}
	}
	total := len(clients)
	if !exists {
		total++
	}
	return client, old, total, nil
}

func (s clientStore) insertClient(ctx context.Context, user gocql.UUID, client storedClient, keys []uncheckedPrekey) error {
	if err := s.updatePrekeys(ctx, user, client.ID, keys); err != nil {
		return err
	}
	if err := s.mutation(ctx, `INSERT INTO clients (user,client,tstamp,type,label,class,cookie,model,capabilities) VALUES (?,?,?,?,?,?,?,?,?)`, user, client.ID, client.Created, client.Type, client.Label, client.Class, client.Cookie, client.Model, client.Capabilities); err != nil {
		return err
	}
	return s.addMLSPublicKeys(ctx, user, client.ID, client.PublicKeys)
}
