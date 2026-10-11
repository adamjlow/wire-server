// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"

	"github.com/apache/cassandra-gocql-driver/v2"
)

var errDeleteUserInvalid = errors.New("invalid deletion user")
var errDeleteUserOwner = errors.New("team owner cannot delete self")

// Account deletion follows the native nontransactional effect order. Dependencies
// are explicit so a failed phase cannot be replaced by a success response.
// Internal queue replay calls execute even for a tombstoned account; public self
// deletion deliberately returns immediately for that status.
type accountDeletionService struct {
	get          func(context.Context, gocql.UUID) (*storedUser, error)
	owner        func(context.Context, gocql.UUID, gocql.UUID, string) (bool, error)
	password     func(context.Context, gocql.UUID) (*string, error)
	verify       func(context.Context, gocql.UUID, string, string) (bool, error)
	sendCode     func(context.Context, storedUser, string) (int64, error)
	emailKey     func(context.Context, string, *gocql.UUID) error
	properties   func(context.Context, gocql.UUID) error
	tombstone    func(context.Context, storedUser) error
	invitations  func(context.Context, storedUser) error
	groups       func(context.Context, gocql.UUID, gocql.UUID, string) error
	peers        func(context.Context, storedUser, string) error
	clients      func(context.Context, gocql.UUID) ([]storedClient, error)
	removeClient func(context.Context, gocql.UUID, string) error
	index        func(context.Context, gocql.UUID, string) error
	events       func(context.Context, gocql.UUID, string) error
	connections  func(context.Context, gocql.UUID) error
	cookies      func(context.Context, gocql.UUID) error
}

func (s accountDeletionService) self(ctx context.Context, id gocql.UUID, password *string, requestID string) (*int64, error) {
	if s.get == nil {
		return nil, errors.New("account deletion lookup unavailable")
	}
	user, err := s.get(ctx, id)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errDeleteUserInvalid
	}
	status := int32(0)
	if user.Status != nil {
		status = *user.Status
	}
	if status == 2 {
		return nil, nil
	}
	if (status == 0 || status == 1) && user.Team != nil {
		if s.owner == nil {
			return nil, errors.New("account deletion owner check unavailable")
		}
		owner, err := s.owner(ctx, *user.Team, id, requestID)
		if err != nil {
			return nil, err
		}
		if owner {
			return nil, errDeleteUserOwner
		}
	}
	if password != nil {
		// Native deletion does not run ordinary reauthentication's status/SAML
		// exceptions and does not upgrade a password that is about to be deleted.
		if s.password == nil || s.verify == nil {
			return nil, errors.New("account deletion password check unavailable")
		}
		encoded, err := s.password(ctx, id)
		if err != nil {
			return nil, err
		}
		if encoded == nil {
			return nil, authInvalidCredentials
		}
		matched, err := s.verify(ctx, id, *encoded, *password)
		if err != nil {
			return nil, err
		}
		if !matched {
			return nil, authInvalidCredentials
		}
	} else if user.Activated && user.Email != nil {
		if s.sendCode == nil {
			return nil, errors.New("account deletion verification unavailable")
		}
		ttl, err := s.sendCode(ctx, *user, requestID)
		if err != nil {
			return nil, err
		}
		return &ttl, nil
	}
	return nil, s.execute(ctx, *user, requestID)
}

func (s accountDeletionService) execute(ctx context.Context, user storedUser, requestID string) error {
	// Check all required dependencies before the first irreversible effect.
	if s.emailKey == nil || s.properties == nil || s.tombstone == nil || s.invitations == nil || s.groups == nil || s.peers == nil || s.clients == nil || s.removeClient == nil || s.index == nil || s.events == nil || s.connections == nil || s.cookies == nil {
		return errors.New("account deletion dependencies unavailable")
	}
	if user.Activated && user.Email != nil {
		if err := s.emailKey(ctx, *user.Email, &user.ID); err != nil {
			return err
		}
	}
	if err := s.properties(ctx, user.ID); err != nil {
		return err
	}
	if err := s.tombstone(ctx, user); err != nil {
		return err
	}
	if user.Team != nil {
		if err := s.invitations(ctx, user); err != nil {
			return err
		}
		if err := s.groups(ctx, user.ID, *user.Team, requestID); err != nil {
			return err
		}
	}
	if err := s.peers(ctx, user, requestID); err != nil {
		return err
	}
	clients, err := s.clients(ctx, user.ID)
	if err != nil {
		return err
	}
	for _, client := range clients {
		// Native account deletion removes client/prekey storage directly, rather
		// than publishing individual public client-delete events or reauthenticating.
		if err := s.removeClient(ctx, user.ID, client.ID); err != nil {
			return err
		}
	}
	if err := s.index(ctx, user.ID, requestID); err != nil {
		return err
	}
	// Contacts must be read/notified before their rows are cleared.
	if err := s.events(ctx, user.ID, requestID); err != nil {
		return err
	}
	if err := s.connections(ctx, user.ID); err != nil {
		return err
	}
	return s.cookies(ctx, user.ID)
}
