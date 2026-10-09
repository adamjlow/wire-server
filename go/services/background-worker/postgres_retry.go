// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// Preserve only SQLSTATE for supervision. Raw PostgreSQL errors can contain
// SQL, values or connection details and must not escape in job diagnostics.
type postgresOperationError struct{ message, state string }

func (e *postgresOperationError) Error() string { return e.message }

func postgresFailure(err error, message string) error {
	var pgerr *pgconn.PgError
	state := ""
	if errors.As(err, &pgerr) {
		state = pgerr.Code
	}
	return &postgresOperationError{message: message, state: state}
}

func retryPostgres(ctx context.Context, reset func(), attempt func() error) error {
	for retries := 0; ; retries++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := attempt()
		var failure *postgresOperationError
		if err == nil || retries == 5 || !errors.As(err, &failure) {
			return err
		}
		switch failure.state {
		case "57P01", "57P02", "57P03", "57P04":
			// Wire.Postgres.useWithResetAndRetry performs five reset/retry
			// transitions, followed by a sixth non-retried session attempt.
			reset()
		default:
			return err
		}
	}
}

func (s *postgresStore) GroupChannels(ctx context.Context, team, group string) (channels []string, found bool, err error) {
	err = retryPostgres(ctx, s.Pool.Reset, func() error {
		var attemptErr error
		channels, found, attemptErr = s.groupChannelsOnce(ctx, team, group)
		return attemptErr
	})
	return
}

func (s *postgresStore) GroupMembers(ctx context.Context, team, group string) (users []string, found bool, err error) {
	err = retryPostgres(ctx, s.Pool.Reset, func() error {
		var attemptErr error
		users, found, attemptErr = s.groupMembersOnce(ctx, team, group)
		return attemptErr
	})
	return
}

func (s *postgresStore) Conversation(ctx context.Context, id string) (conv *storedConversation, err error) {
	err = retryPostgres(ctx, s.Pool.Reset, func() error {
		var attemptErr error
		conv, attemptErr = s.conversationOnce(ctx, id)
		return attemptErr
	})
	return
}

func (s *postgresStore) UpsertLocalMembers(ctx context.Context, conv string, users []string, role string) error {
	return retryPostgres(ctx, s.Pool.Reset, func() error { return s.upsertLocalMembersOnce(ctx, conv, users, role) })
}
