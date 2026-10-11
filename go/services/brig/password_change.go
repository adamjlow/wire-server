// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/apache/cassandra-gocql-driver/v2"
)

var (
	errPasswordChangeIdentity = errors.New("password change requires activated identity")
	errPasswordChangeCurrent  = errors.New("password change current credential rejected")
	errPasswordChangeSame     = errors.New("changed password must differ")
)

type passwordChangeRequest struct {
	old *string
	new string
}

func (passwordChangeRequest) String() string { return "<PasswordChangeRequest>" }

type passwordChangeService struct {
	activated func(context.Context, gocql.UUID) (bool, error)
	current   func(context.Context, gocql.UUID) (*string, error)
	hash      func(context.Context, passwordRateKey, string, int) (string, error)
	verify    func(context.Context, passwordRateKey, string, string) (bool, bool, error)
	write     func(context.Context, gocql.UUID, string) error
	revoke    func(context.Context, gocql.UUID) error
}

func (s passwordChangeService) change(ctx context.Context, user gocql.UUID, request passwordChangeRequest) error {
	if s.activated == nil || s.current == nil || s.hash == nil || s.write == nil {
		return errors.New("password change dependencies unavailable")
	}
	active, err := s.activated(ctx, user)
	if err != nil {
		return err
	}
	if !active {
		return errPasswordChangeIdentity
	}
	current, err := s.current(ctx, user)
	if err != nil {
		return err
	}
	rate := passwordRateKey{kind: "user", id: user.String()}
	// Native hashes before checking missing/wrong/same old passwords. Preserve
	// its rate accounting, error precedence and work order.
	hashed, err := s.hash(ctx, rate, request.new, 8)
	if err != nil {
		return err
	}
	if current == nil {
		return s.write(ctx, user, hashed)
	}
	if request.old == nil {
		return errPasswordChangeCurrent
	}
	if s.verify == nil || s.revoke == nil {
		return errors.New("password change verification unavailable")
	}
	valid, _, err := s.verify(ctx, rate, *current, *request.old)
	if err != nil {
		return err
	}
	if !valid {
		return errPasswordChangeCurrent
	}
	same, _, err := s.verify(ctx, rate, *current, request.new)
	if err != nil {
		return err
	}
	if same {
		return errPasswordChangeSame
	}
	if err := s.write(ctx, user, hashed); err != nil {
		return err
	}
	return s.revoke(ctx, user)
}

func decodePasswordChange(reader io.Reader) (passwordChangeRequest, error) {
	var request passwordChangeRequest
	var raw json.RawMessage
	decoder := json.NewDecoder(reader)
	var trailing any
	if decoder.Decode(&raw) != nil || decoder.Decode(&trailing) != io.EOF || !validRegistrationJSONText(raw) {
		return request, errors.New("invalid password change request")
	}
	fields, err := registrationObject(raw)
	if err != nil {
		return request, err
	}
	request.old, err = registrationText(fields, "old_password", false)
	if err != nil {
		return request, err
	}
	if request.old != nil {
		if err := validatePlainPassword(*request.old, 6); err != nil {
			return request, err
		}
	}
	value, err := registrationText(fields, "new_password", true)
	if err != nil {
		return request, err
	}
	if err := validatePlainPassword(*value, 8); err != nil {
		return request, err
	}
	request.new = *value
	return request, nil
}

func (s passwordChangeService) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodHead && r.Method != http.MethodPut {
		w.WriteHeader(405)
		return
	}
	user, valid := userHeader(w, r)
	if !valid {
		return
	}
	if r.Method == http.MethodHead {
		if s.current == nil {
			registrationHTTPError(w, errors.New("password read unavailable"))
			return
		}
		current, err := s.current(r.Context(), user)
		if err != nil {
			registrationHTTPError(w, err)
		} else if current == nil {
			w.WriteHeader(404)
		} else {
			w.WriteHeader(200)
		}
		return
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(media, "application/json") {
		w.WriteHeader(415)
		return
	}
	request, err := decodePasswordChange(r.Body)
	if err != nil {
		wireError(w, 400, "bad-request", "Invalid password change request")
		return
	}
	err = s.change(r.Context(), user, request)
	switch {
	case err == nil:
		w.WriteHeader(200)
	case errors.Is(err, errPasswordChangeIdentity):
		wireError(w, 403, "no-identity", "The user has no verified email")
	case errors.Is(err, errPasswordChangeCurrent):
		wireError(w, 403, "invalid-credentials", "Authentication failed")
	case errors.Is(err, errPasswordChangeSame):
		wireError(w, 409, "password-must-differ", "For password change, new and old password must be different.")
	default:
		registrationHTTPError(w, err)
	}
}
