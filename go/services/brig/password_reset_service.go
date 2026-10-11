// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"crypto/subtle"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

var (
	errPasswordResetKey   = errors.New("invalid reset identity")
	errPasswordResetCode  = errors.New("invalid reset code")
	errPasswordResetSame  = errors.New("reset password must differ")
	errPasswordResetPhone = errors.New("unsupported reset phone")
)

const passwordResetTTL int32 = 3600

type passwordResetCodes interface {
	lookup(context.Context, string) (*passwordResetRecord, error)
	insert(context.Context, string, passwordResetRecord, int32) error
	delete(context.Context, string) error
}

type passwordResetRequest struct {
	key, email, phone *string
	code, password    string
}

func (passwordResetRequest) String() string { return "<PasswordResetRequest>" }

type passwordResetService struct {
	codes           passwordResetCodes
	activeEmail     func(context.Context, string) (*storedUser, error)
	account         func(context.Context, gocql.UUID) (*storedUser, error)
	currentPassword func(context.Context, gocql.UUID) (*string, error)
	setPassword     func(context.Context, gocql.UUID, string) error
	deleteCookies   func(context.Context, gocql.UUID) error
	verifyPassword  func(context.Context, passwordRateKey, string, string) (bool, bool, error)
	hashPassword    func(context.Context, passwordRateKey, string, int) (string, error)
	mail            func(context.Context, string, string, string, string) error
	allowDomains    *[]string
	domain, locale  string
	newCode         func() (string, error)
	now             func() time.Time
}

func (s passwordResetService) pending(ctx context.Context, user gocql.UUID) (*passwordResetRecord, error) {
	at := s.now()
	row, err := s.codes.lookup(ctx, passwordResetKey(user))
	if err != nil || row == nil {
		return nil, err
	}
	if row.timeout == nil || !row.timeout.After(at) {
		return nil, nil
	}
	return row, nil
}

func (s passwordResetService) begin(ctx context.Context, request passwordResetRequest) error {
	if request.phone != nil {
		return errPasswordResetPhone
	}
	if request.email == nil || s.activeEmail == nil || s.codes == nil || s.now == nil {
		return errors.New("reset dependencies unavailable")
	}
	email := *request.email
	// These native admission errors are deliberately swallowed to avoid
	// revealing account existence, SAML policy or pending resets.
	if s.allowDomains != nil && !slices.Contains(*s.allowDomains, email[strings.LastIndexByte(email, '@')+1:]) {
		return nil
	}
	user, err := s.activeEmail(ctx, email)
	if err != nil || user == nil {
		return err
	}
	saml, err := storedSAML(user)
	if err != nil || saml {
		return err
	}
	pending, err := s.pending(ctx, user.ID)
	if err != nil || pending != nil {
		return err
	}
	if s.newCode == nil || s.mail == nil {
		return errors.New("reset delivery unavailable")
	}
	code, err := s.newCode()
	if err != nil {
		return err
	}
	key := passwordResetKey(user.ID)
	retries, timeout := int32(3), s.now().Add(time.Duration(passwordResetTTL)*time.Second)
	row := passwordResetRecord{code: code, user: user.ID, retries: &retries, timeout: &timeout}
	if err := s.codes.insert(ctx, key, row, passwordResetTTL); err != nil {
		return err
	}
	profile, err := user.selfProfile(s.domain, s.locale)
	if err != nil {
		return err
	}
	return s.mail(ctx, email, key, code, profile.Locale)
}

func (s passwordResetService) pair(ctx context.Context, email string) (map[string]string, error) {
	if s.activeEmail == nil || s.codes == nil || s.now == nil {
		return nil, errors.New("reset lookup unavailable")
	}
	user, err := s.activeEmail(ctx, email)
	if err != nil || user == nil {
		return nil, err
	}
	row, err := s.pending(ctx, user.ID)
	if err != nil || row == nil {
		return nil, err
	}
	return map[string]string{"key": passwordResetKey(user.ID), "code": row.code}, nil
}

func (s passwordResetService) complete(ctx context.Context, request passwordResetRequest) error {
	if s.codes == nil || s.now == nil {
		return errors.New("reset verification unavailable")
	}
	var key string
	switch {
	case request.key != nil:
		key = *request.key
	case request.email != nil:
		if s.activeEmail == nil {
			return errors.New("reset account lookup unavailable")
		}
		user, err := s.activeEmail(ctx, *request.email)
		if err != nil {
			return err
		}
		if user == nil {
			return errPasswordResetKey
		}
		key = passwordResetKey(user.ID)
	case request.phone != nil:
		return errPasswordResetPhone
	default:
		return errPasswordResetKey
	}
	at := s.now()
	row, err := s.codes.lookup(ctx, key)
	if err != nil {
		return err
	}
	if row == nil {
		return errPasswordResetCode
	}
	valid := subtle.ConstantTimeCompare([]byte(row.code), []byte(request.code)) == 1 && row.timeout != nil && !row.timeout.Before(at)
	if !valid {
		if row.retries != nil && *row.retries > 1 && row.timeout != nil && row.timeout.After(at) {
			remaining := *row.retries - 1
			copy := *row
			copy.retries = &remaining
			// Native rewrites the complete row with a fresh one-hour storage
			// TTL while preserving the original logical timeout.
			err = s.codes.insert(ctx, key, copy, passwordResetTTL)
		} else {
			err = s.codes.delete(ctx, key)
		}
		if err != nil {
			return err
		}
		return errPasswordResetCode
	}
	if s.account == nil || s.currentPassword == nil || s.verifyPassword == nil || s.hashPassword == nil || s.setPassword == nil || s.deleteCookies == nil {
		return errors.New("reset mutation unavailable")
	}
	user, err := s.account(ctx, row.user)
	if err != nil {
		return err
	}
	saml, err := storedSAML(user)
	if err != nil {
		return err
	}
	if saml {
		return errPasswordResetCode
	}
	current, err := s.currentPassword(ctx, row.user)
	if err != nil {
		return err
	}
	rate := passwordRateKey{kind: "user", id: row.user.String()}
	if current != nil {
		matches, _, err := s.verifyPassword(ctx, rate, *current, request.password)
		if err != nil {
			return err
		}
		if matches {
			return errPasswordResetSame
		}
	}
	hashed, err := s.hashPassword(ctx, rate, request.password, 8)
	if err != nil {
		return err
	}
	if err = s.setPassword(ctx, row.user, hashed); err != nil {
		return err
	}
	if err = s.codes.delete(ctx, key); err != nil {
		return err
	}
	return s.deleteCookies(ctx, row.user)
}
