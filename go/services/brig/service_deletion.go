// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"

	"github.com/apache/cassandra-gocql-driver/v2"
	"golang.org/x/sync/errgroup"
)

type deletionServiceRecord struct {
	name string
	tags []int64
}

type deletionServiceBot struct {
	user, conversation gocql.UUID
}

// Native service deletion leaves metadata until all bot and peer work succeeds,
// so a queue retry can finish a partially completed deletion.
type serviceDeletion struct {
	lookup     func(context.Context, gocql.UUID, gocql.UUID) (*deletionServiceRecord, error)
	pages      func(context.Context, gocql.UUID, gocql.UUID, func([]deletionServiceBot) error) error
	kick       func(context.Context, deletionServiceBot) error
	disconnect func(context.Context, gocql.UUID, gocql.UUID) error
	remove     func(context.Context, gocql.UUID, gocql.UUID, deletionServiceRecord) error
}

func (s serviceDeletion) execute(ctx context.Context, provider, service gocql.UUID) error {
	if s.lookup == nil {
		return errors.New("service deletion lookup unavailable")
	}
	record, err := s.lookup(ctx, provider, service)
	if err != nil || record == nil {
		return err
	}
	if s.pages == nil || s.kick == nil || s.disconnect == nil || s.remove == nil {
		return errors.New("service deletion dependencies unavailable")
	}
	err = s.pages(ctx, provider, service, func(bots []deletionServiceBot) error {
		group, work := errgroup.WithContext(ctx)
		group.SetLimit(16)
		for _, bot := range bots {
			if work.Err() != nil {
				break
			}
			group.Go(func() error {
				if err := work.Err(); err != nil {
					return err
				}
				return s.kick(work, bot)
			})
		}
		if err := group.Wait(); err != nil {
			return err
		}
		return ctx.Err()
	})
	if err != nil {
		return err
	}
	if err := s.disconnect(ctx, provider, service); err != nil {
		return err
	}
	return s.remove(ctx, provider, service, *record)
}

// A service bot is removed without the ordinary user-deletion cascade. Native
// retains the user row/identity and MLS public keys while changing its status.
type serviceBotDeletion struct {
	removeMember      func(context.Context, deletionServiceBot) error
	account           func(context.Context, gocql.UUID) (*storedUser, error)
	clients           func(context.Context, gocql.UUID) ([]storedClient, error)
	removeClient      func(context.Context, gocql.UUID, string) error
	removeAssociation func(context.Context, gocql.UUID, gocql.UUID, gocql.UUID) error
	status            func(context.Context, gocql.UUID, int32) error
}

func (s serviceBotDeletion) execute(ctx context.Context, bot deletionServiceBot) error {
	if s.removeMember == nil || s.account == nil || s.clients == nil || s.removeClient == nil || s.removeAssociation == nil || s.status == nil {
		return errors.New("bot deletion dependencies unavailable")
	}
	if err := s.removeMember(ctx, bot); err != nil {
		return err
	}
	account, err := s.account(ctx, bot.user)
	if err != nil {
		return err
	}
	clients, err := s.clients(ctx, bot.user)
	if err != nil {
		return err
	}
	for _, client := range clients {
		if err := s.removeClient(ctx, bot.user, client.ID); err != nil {
			return err
		}
	}
	// Native account lookup excludes pending invitations. Client removal and
	// status update still run even when no account survives that projection.
	if account != nil && (account.Status == nil || *account.Status != 4) && account.Provider != nil && account.Service != nil {
		if err := s.removeAssociation(ctx, *account.Provider, *account.Service, bot.user); err != nil {
			return err
		}
	}
	return s.status(ctx, bot.user, 2)
}
