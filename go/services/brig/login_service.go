// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type loginFailure string

func (f loginFailure) Error() string { return string(f) }

const (
	loginFailed       loginFailure = "login failed"
	loginPending      loginFailure = "login pending activation"
	loginCodeRequired loginFailure = "login verification code required"
	loginCodeInvalid  loginFailure = "login verification code invalid"
)

type loginBlocked struct{ seconds int64 }

func (loginBlocked) Error() string { return "login retry budget exhausted" }

type failedLoginLimit struct {
	Timeout float64 `yaml:"timeout"`
	Retries int64   `yaml:"retryLimit"`
}
type loginRequest struct {
	identifier  loginIdentifier
	password    string
	label, code *string
	persistent  bool
}
type loginService struct {
	resolve             func(context.Context, loginIdentifier) (*gocql.UUID, error)
	pending             func(context.Context, string) (bool, error)
	authenticate        func(context.Context, gocql.UUID, string) error
	account             func(context.Context, gocql.UUID) (*storedUser, error)
	verificationEnabled func(context.Context, string, string) (bool, error)
	verifyCode          func(context.Context, string, string, string) (bool, error)
	budgets             loginBudgetStorage
	limit               *failedLoginLimit
	lifecycle           accountLifecycle
	inactiveTimeout     *time.Duration
	sessions            sessionManager
	now                 func() time.Time
}

func (s loginService) budget(ctx context.Context, user gocql.UUID, decrement bool) error {
	if s.limit == nil {
		return nil
	}
	key := "login#" + user.String()
	stored, err := s.budgets.lookup(ctx, key)
	if err != nil {
		return err
	}
	budget := loginBudget{seconds: s.limit.Timeout, remaining: int32(s.limit.Retries)}
	if stored != nil {
		budget = *stored
	}
	remaining := budget.remaining - 1 // Native Int32 subtraction also wraps.
	if remaining < 0 {
		return loginBlocked{seconds: int64(math.Floor(budget.seconds))}
	}
	if decrement {
		budget.remaining = remaining
		return s.budgets.insert(ctx, key, budget)
	}
	return nil
}

func (s loginService) failedAttempt(ctx context.Context, user gocql.UUID, failure error) error {
	// Native ignores a concurrently exhausted decrement, but storage errors
	// still propagate. The pre-authentication check alone returns LoginBlocked.
	var blocked loginBlocked
	if err := s.budget(ctx, user, true); err != nil && !errors.As(err, &blocked) {
		return err
	}
	return failure
}

func (s loginService) login(ctx context.Context, request loginRequest, requestID string) (renewedAccess, error) {
	user, err := s.resolve(ctx, request.identifier)
	if err != nil {
		return renewedAccess{}, err
	}
	if user == nil {
		if request.identifier.email != nil {
			pending, err := s.pending(ctx, *request.identifier.email)
			if err != nil {
				return renewedAccess{}, err
			}
			if pending {
				return renewedAccess{}, loginPending
			}
		}
		return renewedAccess{}, loginFailed
	}
	if err := s.budget(ctx, *user, false); err != nil {
		return renewedAccess{}, err
	}
	if err := s.authenticate(ctx, *user, request.password); err != nil {
		if errors.Is(err, authInvalidCredentials) {
			err = s.failedAttempt(ctx, *user, loginFailed)
		}
		return renewedAccess{}, err
	}
	account, err := s.account(ctx, *user)
	if err != nil {
		return renewedAccess{}, err
	}
	required := false
	if account != nil && account.Team != nil {
		required, err = s.verificationEnabled(ctx, account.Team.String(), requestID)
		if err != nil {
			return renewedAccess{}, err
		}
	}
	saml, err := storedSAML(account)
	if err != nil {
		return renewedAccess{}, err
	}
	if required && !saml {
		if request.code == nil {
			return renewedAccess{}, s.failedAttempt(ctx, *user, loginCodeRequired)
		}
		if account == nil || !account.Activated || account.Email == nil {
			return renewedAccess{}, s.failedAttempt(ctx, *user, loginFailed)
		}
		matched, err := s.verifyCode(ctx, *account.Email, "login", *request.code)
		if err != nil {
			return renewedAccess{}, err
		}
		if !matched {
			return renewedAccess{}, s.failedAttempt(ctx, *user, loginCodeInvalid)
		}
	}
	if err := s.lifecycle.suspendInactive(ctx, *user, s.inactiveTimeout, s.now()); err != nil {
		if errors.Is(err, ErrExpiredToken) {
			err = authSuspended
		}
		return renewedAccess{}, err
	}
	typ := int32(0)
	if request.persistent {
		typ = 1
	}
	cookie, err := s.sessions.limitedCookie(ctx, *user, nil, typ, request.label, false, true, s.now())
	if err != nil {
		return renewedAccess{}, err
	}
	token, ttl, err := s.sessions.issuer.access(cookie.token, nil, s.now())
	return renewedAccess{token: token, expiresIn: ttl, cookie: &cookie}, err
}
