// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"
	"unicode/utf8"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type passwordPolicy struct {
	Algorithm   string `yaml:"algorithm"`
	Memory      uint32 `yaml:"memory"`
	Iterations  uint32 `yaml:"iterations"`
	Parallelism uint32 `yaml:"parallelism"`
}

func (p passwordPolicy) validate() error {
	if p.Algorithm == "scrypt" {
		return nil
	}
	if p.Algorithm != "argon2id" || p.Iterations == 0 || p.Parallelism == 0 || uint64(p.Memory) < 8*uint64(p.Parallelism) {
		return errors.New("invalid password hashing configuration")
	}
	return nil
}

// Limit simultaneous C hashing operations without changing stored hash
// parameters. Cancellation releases queued work before it starts; an active
// synchronous C operation completes before releasing its slot.
type passwordService struct {
	policy passwordPolicy
	rate   *passwordRateLimiter
	slots  chan struct{}
}

func newPasswordService(policy passwordPolicy, rate *passwordRateLimiter, workers int) (*passwordService, error) {
	if err := policy.validate(); err != nil {
		return nil, err
	}
	if rate == nil || workers < 1 {
		return nil, errors.New("invalid password service configuration")
	}
	return &passwordService{policy: policy, rate: rate, slots: make(chan struct{}, workers)}, nil
}

func validatePlainPassword(plain string, minimum int) error {
	length := utf8.RuneCountInString(plain)
	if !utf8.ValidString(plain) || length < minimum || length > 1024 {
		return errors.New("invalid password length")
	}
	return nil
}

func (p *passwordService) begin(ctx context.Context, key passwordRateKey) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if wait := p.rate.check(key); wait != 0 {
		return nil, passwordRateExceeded{wait}
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case p.slots <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-p.slots
			return nil, err
		}
		return func() { <-p.slots }, nil
	}
}

func (p *passwordService) verify(ctx context.Context, key passwordRateKey, encoded, plain string) (bool, bool, error) {
	if err := validatePlainPassword(plain, 6); err != nil {
		return false, false, err
	}
	finish, err := p.begin(ctx, key)
	if err != nil {
		return false, false, err
	}
	defer finish()
	return verifyPasswordHash(encoded, plain, p.policy.Algorithm == "argon2id")
}

func (p *passwordService) hash(ctx context.Context, key passwordRateKey, plain string, minimum int) (string, error) {
	if err := validatePlainPassword(plain, minimum); err != nil {
		return "", err
	}
	finish, err := p.begin(ctx, key)
	if err != nil {
		return "", err
	}
	defer finish()
	return newPasswordHash(plain, p.policy.Algorithm == "argon2id", p.policy.Memory, p.policy.Iterations, p.policy.Parallelism)
}

type authenticationStorage interface {
	authentication(context.Context, gocql.UUID) (*userAuthentication, error)
	setPassword(context.Context, gocql.UUID, string) error
}

type authenticationFailure string

func (f authenticationFailure) Error() string { return string(f) }

const (
	authInvalidCredentials authenticationFailure = "authentication failed"
	authSuspended          authenticationFailure = "account suspended"
	authEphemeral          authenticationFailure = "ephemeral account"
	authPendingInvitation  authenticationFailure = "pending invitation"
	authMissingPassword    authenticationFailure = "missing authentication"
)

type authenticationService struct {
	store     authenticationStorage
	passwords *passwordService
	isSAML    func(context.Context, gocql.UUID) (bool, error)
}

func authenticationStatus(record *userAuthentication, reauth bool) error {
	if record == nil || record.Status == 2 {
		return authInvalidCredentials
	}
	switch record.Status {
	case 0:
		return nil
	case 1:
		return authSuspended
	case 3:
		if reauth {
			return nil
		}
		return authEphemeral
	case 4:
		return authPendingInvitation
	default:
		return errors.New("invalid stored account status")
	}
}

func (a authenticationService) authenticate(ctx context.Context, user gocql.UUID, plain string) error {
	record, err := a.store.authentication(ctx, user)
	if err != nil {
		return err
	}
	if err := authenticationStatus(record, false); err != nil {
		return err
	}
	if record.Password == nil {
		return authInvalidCredentials
	}
	key := passwordRateKey{kind: "user", id: user.String()}
	matched, upgrade, err := a.passwords.verify(ctx, key, *record.Password, plain)
	if err != nil {
		return err
	}
	if !matched {
		return authInvalidCredentials
	}
	if upgrade {
		encoded, err := a.passwords.hash(ctx, key, plain, 6)
		var throttled passwordRateExceeded
		if errors.As(err, &throttled) {
			return nil
		}
		if err != nil {
			return err
		}
		return a.store.setPassword(ctx, user, encoded)
	}
	return nil
}

func (a authenticationService) reauthenticate(ctx context.Context, user gocql.UUID, plain *string) error {
	record, err := a.store.authentication(ctx, user)
	if err != nil {
		return err
	}
	if err := authenticationStatus(record, true); err != nil {
		return err
	}
	if record.Password == nil {
		if plain != nil {
			return authInvalidCredentials
		}
		return nil
	}
	if plain == nil {
		if a.isSAML == nil {
			return errors.New("SAML account lookup unavailable")
		}
		saml, err := a.isSAML(ctx, user)
		if err != nil {
			return err
		}
		if !saml {
			return authMissingPassword
		}
		return nil
	}
	matched, _, err := a.passwords.verify(ctx, passwordRateKey{kind: "user", id: user.String()}, *record.Password, *plain)
	if err != nil {
		return err
	}
	if !matched {
		return authInvalidCredentials
	}
	return nil
}
