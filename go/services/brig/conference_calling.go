// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/jackc/pgx/v5"
)

// Galley's complete feature lookup calls back into this Brig read. Preserve
// native null/missing-row fallback and the same user migration lock/selection.
func (s userStore) conferenceCalling(ctx context.Context, user gocql.UUID) (*int32, error) {
	var status *int32
	err := s.withUserBackend(ctx, user, func(postgres bool) error {
		if postgres {
			return s.withPostgres(ctx, func(conn *pgx.Conn) error {
				err := conn.QueryRow(ctx, `SELECT feature_conference_calling FROM wire_user WHERE id=$1::uuid`, user.String()).Scan(&status)
				if errors.Is(err, pgx.ErrNoRows) {
					return nil
				}
				return err
			})
		}
		err := s.cassandra.Query(`SELECT feature_conference_calling FROM user WHERE id=?`, user).
			WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).Scan(&status)
		if errors.Is(err, gocql.ErrNotFound) {
			return nil
		}
		return err
	})
	return status, userStorageError(err)
}

func conferenceCallingStatus(c Config, stored *int32) (string, error) {
	if stored != nil {
		switch *stored {
		case 0:
			return "disabled", nil
		case 1:
			return "enabled", nil
		default:
			return "", errors.New("invalid stored conference calling status")
		}
	}
	if flags := c.Settings.FeatureFlags; flags != nil && flags.ConferenceCalling.DefaultForNull != nil {
		status := flags.ConferenceCalling.DefaultForNull.Status
		if status != "disabled" && status != "enabled" {
			return "", errors.New("invalid conference calling default")
		}
		return status, nil
	}
	return "enabled", nil
}
