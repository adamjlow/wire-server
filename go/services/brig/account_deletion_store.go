// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/jackc/pgx/v5"
	"golang.org/x/sync/errgroup"
)

func (s userStore) deleteAccount(ctx context.Context, user storedUser) error {
	return s.withUserBackend(ctx, user.ID, func(postgres bool) error {
		if postgres {
			return s.withPostgres(ctx, func(conn *pgx.Conn) error {
				return migrationTransaction(ctx, conn, func(tx pgx.Tx) error {
					var created time.Time
					err := tx.QueryRow(ctx, `DELETE FROM wire_user WHERE id=$1::uuid RETURNING created_at`, user.ID.String()).Scan(&created)
					if errors.Is(err, pgx.ErrNoRows) {
						return nil
					}
					if err != nil {
						return userStorageError(err)
					}
					_, err = tx.Exec(ctx, `INSERT INTO deleted_user (id,team,created_at) VALUES ($1::uuid,$2::uuid,$3) ON CONFLICT (id) DO NOTHING`, user.ID.String(), uuidText(user.Team), created)
					return userStorageError(err)
				})
			})
		}
		if user.Handle != nil {
			owner, err := s.cassandraLoginLookup(ctx, `SELECT user FROM user_handle WHERE handle=?`, *user.Handle)
			if err != nil {
				return err
			}
			if owner != nil && *owner == user.ID {
				if err := s.deleteQuery(ctx, 5, `DELETE FROM user_handle WHERE handle=?`, *user.Handle).Exec(); err != nil {
					return userStorageError(err)
				}
				if err := s.deleteQuery(ctx, 5, `UPDATE unique_claims USING TTL ? SET claims = claims - ? WHERE value = ?`, int32(3600), []gocql.UUID{user.ID}, "@"+*user.Handle).Exec(); err != nil {
					return userStorageError(err)
				}
			}
		}
		return userStorageError(s.deleteQuery(ctx, 5, `UPDATE user SET status=?, name=?, accent_id=?, picture=?, assets=?, handle=null, country=null, language=null, email=null, sso_id=null WHERE id=?`, int32(2), "default", int32(0), [][]byte{}, []userAsset{}, user.ID).Exec())
	})
}
func (s userStore) deleteQuery(ctx context.Context, retries int, query string, values ...any) *gocql.Query {
	return s.cassandra.Query(query, values...).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: retries})
}

func (s userStore) deleteAccountInvitations(ctx context.Context, user storedUser) error {
	if user.Team == nil {
		return nil
	}
	if err := (invitationStore{s.cassandra}).delete(ctx, *user.Team, user.ID); err != nil {
		return err
	}
	if user.Activated && user.Email != nil {
		return userStorageError(s.deleteQuery(ctx, 5, `DELETE FROM team_scim_pending_user_email WHERE team=? AND email=? AND user=?`, *user.Team, *user.Email, user.ID).Exec())
	}
	return nil
}

func (s connectionStore) deleteAccount(ctx context.Context, user gocql.UUID) error {
	var state []byte
	for {
		iter := s.cassandra.Query(`SELECT right,status FROM connection WHERE left=?`, user).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).PageSize(100).PageState(state).Iter()
		contacts := []gocql.UUID{}
		var other *gocql.UUID
		var status *int32
		for iter.Scan(&other, &status) {
			if other == nil || status == nil || *status < 0 || *status > 11 {
				_ = iter.Close()
				return errors.New("invalid connection cleanup row")
			}
			contacts = append(contacts, *other)
		}
		state = append([]byte{}, iter.PageState()...)
		if err := iter.Close(); err != nil {
			return userStorageError(err)
		}
		group, workerCtx := errgroup.WithContext(ctx)
		group.SetLimit(16)
		for _, contact := range contacts {
			group.Go(func() error {
				// Native reverse-row writes have no retry wrapper.
				return userStorageError(s.cassandra.Query(`DELETE FROM connection WHERE left=? AND right=?`, contact, user).WithContext(workerCtx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 0}).Exec())
			})
		}
		if err := group.Wait(); err != nil {
			return err
		}
		if len(state) == 0 {
			break
		}
	}
	for _, table := range []string{"connection", "connection_remote"} {
		// Only these two compile-time table names enter the query.
		if err := s.cassandra.Query(`DELETE FROM `+table+` WHERE left=?`, user).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).Exec(); err != nil {
			return userStorageError(err)
		}
	}
	return nil
}
