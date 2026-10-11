// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type accountStatusStorage interface {
	exists(context.Context, gocql.UUID) (bool, error)
	updateAccountStatus(context.Context, gocql.UUID, int32) error
}

var (
	errAccountNotFound      = errors.New("account not found")
	errInvalidAccountStatus = errors.New("invalid account status transition")
)

type accountLifecycle struct {
	accounts  accountStatusStorage
	cookies   cookieStorage
	syncIndex func(context.Context, gocql.UUID) error
}

func (a accountLifecycle) changeStatus(ctx context.Context, user gocql.UUID, status int32) error {
	exists, err := a.accounts.exists(ctx, user)
	if err != nil {
		return err
	}
	if !exists {
		return errAccountNotFound
	}
	if status != 0 && status != 1 {
		return errInvalidAccountStatus
	}
	// Require the real index path before any mutation. Native suspension and
	// resumption events have no notification or journal side effects.
	if a.syncIndex == nil {
		return errors.New("account search index unavailable")
	}
	if status == 1 {
		if err := a.cookies.deleteAll(ctx, user); err != nil {
			return err
		}
	}
	if err := a.accounts.updateAccountStatus(ctx, user, status); err != nil {
		return err
	}
	return a.syncIndex(ctx, user)
}

// Cookie creation, rather than expiry or renewal request time, determines
// inactivity. Both cookie types count and equality remains active.
func (a accountLifecycle) suspendInactive(ctx context.Context, user gocql.UUID, timeout *time.Duration, now time.Time) error {
	if timeout == nil {
		return nil
	}
	cookies, err := a.cookies.list(ctx, user)
	if err != nil || len(cookies) == 0 {
		return err
	}
	threshold := now.Add(-*timeout)
	for _, cookie := range cookies {
		if !cookie.Created.Before(threshold) {
			return nil
		}
	}
	err = a.changeStatus(ctx, user, 1)
	if err != nil && !errors.Is(err, errAccountNotFound) && !errors.Is(err, errInvalidAccountStatus) {
		return err
	}
	return ErrExpiredToken
}
