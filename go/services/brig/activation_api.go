// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"

	"github.com/apache/cassandra-gocql-driver/v2"
)

// HTTP wiring must provide real implementations for all side effects. The
// core alone cannot stand in for the public activation API.
type activationAPI struct {
	codes            activationCodeLookup
	guardDomain      func(context.Context, string) error
	activate         func(context.Context, string, string, *gocql.UUID) (*activatedIdentity, error)
	syncIndex        func(context.Context, gocql.UUID) error
	generateEvent    func(context.Context, *activatedIdentity) error
	clearUnvalidated func(context.Context, gocql.UUID) error
	activateTeam     func(context.Context, gocql.UUID) error
}

func (s activationAPI) run(ctx context.Context, key, code string, dryrun, verifyDomain bool, override *gocql.UUID) (*activatedIdentity, error) {
	// Native public activation preverification happens even for repeated links,
	// before the account mutation. Dry-run failures still consume code retries.
	if dryrun || verifyDomain {
		if s.codes == nil || s.guardDomain == nil {
			return nil, errors.New("activation preverification unavailable")
		}
		record, err := s.codes.verify(ctx, key, code)
		if err != nil {
			return nil, err
		}
		if record == nil {
			return nil, errInvalidActivationCode
		}
		if err := s.guardDomain(ctx, record.email); err != nil {
			return nil, err
		}
	}
	if dryrun {
		return nil, nil
	}
	if s.activate == nil || s.syncIndex == nil || s.generateEvent == nil || s.clearUnvalidated == nil || s.activateTeam == nil {
		return nil, errors.New("activation dependencies unavailable")
	}
	result, err := s.activate(ctx, key, code, override)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	if err := s.syncIndex(ctx, result.user); err != nil {
		return nil, err
	}
	// Native event generation schedules best-effort notification and performs
	// configured journaling before the later clear/team operation.
	if err := s.generateEvent(ctx, result); err != nil {
		return nil, err
	}
	if result.first {
		if err := s.activateTeam(ctx, result.user); err != nil {
			return nil, err
		}
	} else {
		// This repeats the core's clear operation, matching onActivated ordering.
		if err := s.clearUnvalidated(ctx, result.user); err != nil {
			return nil, err
		}
	}
	return result, nil
}
