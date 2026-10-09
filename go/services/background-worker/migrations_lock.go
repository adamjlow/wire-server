// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"errors"
	"time"
)

func (s *Service) timedExclusiveMigration(ctx context.Context, name string, acquire func(context.Context, func() error) error, action func(context.Context) error) (err error) {
	start := time.Now()
	outcome := "error"
	defer func() { s.observeMigration(name, outcome, time.Since(start)) }()
	err = acquire(ctx, func() error {
		// Native ten-second lock budget is independent from the configured
		// row timeout; row timeout starts only after lock acquisition.
		attempt, cancel := context.WithTimeout(ctx, time.Duration(s.Config.MigrationOptions.Timeout))
		defer cancel()
		err := action(attempt)
		if errors.Is(attempt.Err(), context.DeadlineExceeded) {
			outcome = "timeout"
			return errors.New("migration row timed out")
		}
		return err
	})
	var lockTimeout *migrationLockTimeout
	if errors.As(err, &lockTimeout) {
		outcome = "lock_timeout"
	} else if err == nil {
		outcome = "success"
	}
	return
}
