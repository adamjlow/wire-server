// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/sync/errgroup"
)

type remoteConversationStatus struct {
	Domain       string
	Conversation pgtype.UUID
	Muted        *int32
	MutedRef     *string
	Archived     bool
	ArchivedRef  *string
	Hidden       bool
	HiddenRef    *string
}

const remoteConversationIDs = `SELECT conv_remote_domain,conv_remote_id FROM user_remote_conv WHERE user = ? ORDER BY conv_remote_domain,conv_remote_id`
const remoteConversationIDsAfter = `SELECT conv_remote_domain,conv_remote_id FROM user_remote_conv WHERE user = ? AND (conv_remote_domain,conv_remote_id) > (?,?) ORDER BY conv_remote_domain,conv_remote_id`
const remoteConversationStatuses = `SELECT conv_remote_id,otr_muted_status,otr_muted_ref,otr_archived,otr_archived_ref,hidden,hidden_ref FROM user_remote_conv WHERE user = ? AND conv_remote_domain = ? AND conv_remote_id IN ?`

var remoteStatusColumns = []string{"conv_remote_id", "otr_muted_status", "otr_muted_ref", "otr_archived", "otr_archived_ref", "hidden", "hidden_ref"}

func decodeRemoteConversationStatus(domain string, row migrationRow) (remoteConversationStatus, error) {
	invalid := errors.New("invalid stored remote conversation membership")
	status := remoteConversationStatus{Domain: strings.ToLower(domain)}
	id, ok := cqlUUID(row["conv_remote_id"])
	if !ok || !id.Valid || !validDomain(domain) {
		return status, invalid
	}
	status.Conversation = id
	var err error
	status.Muted, err = userMigrationNullableInt32(row["otr_muted_status"])
	if err != nil {
		return status, invalid
	}
	for _, field := range []struct {
		column string
		out    **string
	}{{"otr_muted_ref", &status.MutedRef}, {"otr_archived_ref", &status.ArchivedRef}, {"hidden_ref", &status.HiddenRef}} {
		*field.out, err = userMigrationNullableString(row[field.column])
		if err != nil {
			return status, invalid
		}
	}
	for _, field := range []struct {
		column string
		out    *bool
	}{{"otr_archived", &status.Archived}, {"hidden", &status.Hidden}} {
		value, err := userMigrationNullableBool(row[field.column])
		if err != nil {
			return status, invalid
		}
		if value != nil {
			*field.out = *value
		}
	}
	return status, nil
}

type remoteMembershipQuery func(context.Context, string, []string, int, ...any) ([]migrationRow, error)

func remoteMembershipCassandraQuery(session *gocql.Session) remoteMembershipQuery {
	return func(ctx context.Context, cql string, columns []string, size int, args ...any) ([]migrationRow, error) {
		query := session.Query(cql, args...).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(cassandraRetry(0))
		if size > 0 {
			// Native getRemoteConversationIds fetches a single1001-row page,
			// then advances by the last retained domain/UUID, not PageState.
			query = query.PageSize(size).PageState(nil)
		}
		iter := query.Iter()
		row := nullableCassandraRow(columns...)
		var rows []migrationRow
		for iter.MapScan(row) {
			unwrapCassandraRow(row)
			rows = append(rows, migrationRow(row))
			row = nullableCassandraRow(columns...)
		}
		if err := iter.Close(); err != nil {
			return nil, errors.New("Cassandra remote membership lookup failed")
		}
		return rows, nil
	}
}

func readRemoteConversationStatuses(ctx context.Context, user pgtype.UUID, query remoteMembershipQuery) ([]remoteConversationStatus, error) {
	if !user.Valid {
		return nil, errors.New("invalid remote membership user")
	}
	const maximum = 1000 // Native getRemoteConversationIds maxBound Range1..1000.
	groups := make(map[string][]gocql.UUID)
	cql := remoteConversationIDs
	args := []any{gocql.UUID(user.Bytes)}
	for {
		rows, err := query(ctx, cql, []string{"conv_remote_domain", "conv_remote_id"}, maximum+1, args...)
		if err != nil {
			return nil, err
		}
		ids := make([]remoteConversationStatus, len(rows))
		// Decode even the lookahead item before accepting the native page.
		for i, row := range rows {
			domain, err := userMigrationString(row["conv_remote_domain"])
			if err != nil {
				return nil, errors.New("invalid remote conversation domain")
			}
			ids[i], err = decodeRemoteConversationStatus(domain, migrationRow{"conv_remote_id": row["conv_remote_id"]})
			if err != nil {
				return nil, err
			}
		}
		retained := ids[:min(len(ids), maximum)]
		for _, id := range retained {
			groups[id.Domain] = append(groups[id.Domain], gocql.UUID(id.Conversation.Bytes))
		}
		if len(ids) <= maximum {
			break
		}
		last := retained[len(retained)-1]
		cql, args = remoteConversationIDsAfter, []any{gocql.UUID(user.Bytes), last.Domain, gocql.UUID(last.Conversation.Bytes)}
	}
	group, attempt := errgroup.WithContext(ctx)
	group.SetLimit(8) // Native remoteConversationStatus domain bucket pool.
	var mu sync.Mutex
	statuses := make(map[string]remoteConversationStatus)
	for domain, ids := range groups {
		group.Go(func() error {
			if err := attempt.Err(); err != nil {
				return err
			}
			rows, err := query(attempt, remoteConversationStatuses, remoteStatusColumns, 0, gocql.UUID(user.Bytes), domain, ids)
			if err != nil {
				return err
			}
			decoded := make([]remoteConversationStatus, len(rows))
			for i, row := range rows {
				decoded[i], err = decodeRemoteConversationStatus(domain, row)
				if err != nil {
					return err
				}
			}
			mu.Lock()
			defer mu.Unlock()
			for _, status := range decoded {
				statuses[postgresUUIDString(status.Conversation)+"/"+status.Domain] = status
			}
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}
	// Native Map.foldrWithKey yields ascending qualified-conversation order.
	keys := make([]string, 0, len(statuses))
	for key := range statuses {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]remoteConversationStatus, 0, len(keys))
	for _, key := range keys {
		result = append(result, statuses[key])
	}
	return result, nil
}

func writeRemoteConversationStatuses(ctx context.Context, tx userMigrationSQL, user pgtype.UUID, rows []remoteConversationStatus) error {
	users, domains, conversations := make([]pgtype.UUID, len(rows)), make([]string, len(rows)), make([]pgtype.UUID, len(rows))
	muted := make([]*int32, len(rows))
	mutedRef, archivedRef, hiddenRef := make([]*string, len(rows)), make([]*string, len(rows)), make([]*string, len(rows))
	archived, hidden := make([]bool, len(rows)), make([]bool, len(rows))
	for i, row := range rows {
		users[i], domains[i], conversations[i] = user, row.Domain, row.Conversation
		muted[i], mutedRef[i], archived[i], archivedRef[i], hidden[i], hiddenRef[i] = row.Muted, row.MutedRef, row.Archived, row.ArchivedRef, row.Hidden, row.HiddenRef
	}
	if _, err := tx.Exec(ctx, `INSERT INTO remote_conversation_local_member ("user",conv_remote_domain,conv_remote_id,otr_muted_status,otr_muted_ref,otr_archived,otr_archived_ref,hidden,hidden_ref) SELECT * FROM UNNEST($1::uuid[],$2::text[],$3::uuid[],$4::integer[],$5::text[],$6::bool[],$7::text[],$8::bool[],$9::text[])`, users, domains, conversations, muted, mutedRef, archived, archivedRef, hidden, hiddenRef); err != nil {
		return postgresFailure(err, "remote membership migration insert failed")
	}
	// Native intentionally has no membership ON CONFLICT clause. Preserve
	// failure/retry semantics rather than silently ignoring fresher PG rows.
	if _, err := tx.Exec(ctx, `INSERT INTO conversation_migration_pending_deletes (typ,id) VALUES ('user',$1::uuid) ON CONFLICT DO NOTHING`, user); err != nil {
		return postgresFailure(err, "remote membership migration pending write failed")
	}
	return nil
}

func (s *postgresStore) saveRemoteConversationStatuses(ctx context.Context, user pgtype.UUID, rows []remoteConversationStatus) error {
	return retryPostgres(ctx, s.Pool.Reset, func() error {
		conn, err := s.acquire(ctx)
		if err != nil {
			return err
		}
		defer conn.Release()
		_, err = retryUserMigrationTransaction(ctx, func() (bool, error) {
			tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadWrite})
			if err != nil {
				return false, postgresFailure(err, "remote membership migration transaction start failed")
			}
			defer func() { _ = tx.Rollback(ctx) }()
			if err := writeRemoteConversationStatuses(ctx, tx, user, rows); err != nil {
				return false, err
			}
			if err := tx.Commit(ctx); err != nil {
				return false, postgresFailure(err, "remote membership migration transaction commit failed")
			}
			return false, nil
		})
		return err
	})
}

func deleteRemoteConversationStatuses(ctx context.Context, cassandra *gocql.Session, user pgtype.UUID) error {
	if err := cassandra.Query(`DELETE FROM user_remote_conv WHERE user = ?`, gocql.UUID(user.Bytes)).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(cassandraRetry(5)).Exec(); err != nil {
		return errors.New("Cassandra migrated remote membership deletion failed")
	}
	return nil
}

func (s *postgresStore) completeRemoteMembershipMigration(ctx context.Context, user pgtype.UUID) error {
	return s.migrationExec(ctx, `DELETE FROM conversation_migration_pending_deletes WHERE typ='user' AND id=$1::uuid`, user)
}

func (s *Service) copyRemoteConversationStatuses(ctx context.Context,
	lock func(context.Context, func() error) error,
	read func(context.Context) ([]remoteConversationStatus, error),
	save func(context.Context, []remoteConversationStatus) error,
	cleanup, complete func(context.Context) error,
) (bool, error) {
	err := s.timedExclusiveMigration(ctx, "user_remote_convs", lock, func(attempt context.Context) error {
		rows, err := read(attempt)
		if err != nil {
			return err
		}
		if err := save(attempt, rows); err != nil {
			return err
		}
		return cleanup(attempt)
	})
	if err != nil {
		return false, err
	}
	// Source completes this ledger after releasing the UUID lock and row
	// timeout, unlike the local-conversation and user-record migrations.
	if err := complete(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Service) migrateRemoteMemberships(ctx context.Context, cassandra *gocql.Session, postgres *postgresStore) error {
	page := cassandraMigrationPage(cassandra, `SELECT DISTINCT user FROM user_remote_conv`, "user")
	page = validatedMigrationPage(page, func(row migrationRow) error {
		id, ok := cqlUUID(row["user"])
		if !ok || !id.Valid {
			return errors.New("invalid remote membership migration user")
		}
		return nil
	})
	return s.migratePhases(ctx, "user_remote_convs", []migrationPhase{{page: page, parallel: true, write: func(ctx context.Context, row migrationRow) (bool, error) {
		user, _ := cqlUUID(row["user"])
		return s.copyRemoteConversationStatuses(ctx,
			func(ctx context.Context, action func() error) error {
				return postgres.withMigrationLock(ctx, postgresUUIDString(user), true, 10*time.Second, action)
			},
			func(ctx context.Context) ([]remoteConversationStatus, error) {
				return readRemoteConversationStatuses(ctx, user, remoteMembershipCassandraQuery(cassandra))
			},
			func(ctx context.Context, rows []remoteConversationStatus) error {
				return postgres.saveRemoteConversationStatuses(ctx, user, rows)
			},
			func(ctx context.Context) error { return deleteRemoteConversationStatuses(ctx, cassandra, user) },
			func(ctx context.Context) error { return postgres.completeRemoteMembershipMigration(ctx, user) })
	}}})
}
