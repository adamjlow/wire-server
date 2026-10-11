// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"
	"time"
	"unicode/utf8"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type deletionGroupCursor struct {
	id      gocql.UUID
	created time.Time
}
type accountDeletionGroups struct {
	member func(context.Context, gocql.UUID, gocql.UUID, string) (bool, error)
	page   func(context.Context, gocql.UUID, *deletionGroupCursor) ([]deletionGroupCursor, error)
	remove func(context.Context, gocql.UUID, gocql.UUID) error
}

func (s accountDeletionGroups) execute(ctx context.Context, user, team gocql.UUID, requestID string) error {
	if s.member == nil || s.page == nil || s.remove == nil {
		return errors.New("account deletion groups unavailable")
	}
	present, err := s.member(ctx, user, team, requestID)
	if err != nil || !present {
		return err
	}
	var cursor *deletionGroupCursor
	for {
		groups, err := s.page(ctx, team, cursor)
		if err != nil {
			return err
		}
		if len(groups) == 0 {
			return nil
		}
		for _, group := range groups {
			if err := s.remove(ctx, group.id, user); err != nil {
				return err
			}
		}
		last := groups[len(groups)-1]
		cursor = &last
	}
}

// Native group storage is always PostgreSQL, independent of the user selector.
// Replayable reads/deletes retry five administrative-intervention server errors,
// resetting stale pool connections. Do not erase SQLSTATE before this decision.
func (s userAuthStore) deletionGroupPostgres(ctx context.Context, action func(*pgx.Conn) error) error {
	for attempt := 0; ; attempt++ {
		err := s.withPostgres(ctx, action)
		var database *pgconn.PgError
		if attempt >= 5 || ctx.Err() != nil || !errors.As(err, &database) {
			return userStorageError(err)
		}
		switch database.Code {
		case "57P01", "57P02", "57P03", "57P04":
			s.postgres.Reset()
		default:
			return userStorageError(err)
		}
	}
}

func (s userAuthStore) deletionGroupPage(ctx context.Context, team gocql.UUID, cursor *deletionGroupCursor) ([]deletionGroupCursor, error) {
	var groups []deletionGroupCursor
	err := s.deletionGroupPostgres(ctx, func(conn *pgx.Conn) error {
		groups = nil
		tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadOnly})
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		query := `SELECT id::text,name,managed_by,created_at,(SELECT count(*) FROM user_group_channel ugc WHERE ugc.user_group_id=ug.id) FROM user_group ug WHERE ug.team_id=$1::uuid`
		args := []any{team.String()}
		if cursor != nil {
			query += ` AND (ug.created_at,ug.id)<($2::timestamptz,$3::uuid)`
			args = append(args, cursor.created, cursor.id.String())
		}
		query += ` ORDER BY ug.created_at DESC,ug.id DESC LIMIT 15`
		rows, err := tx.Query(ctx, query, args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id, name string
			var managed int32
			var created time.Time
			var channels int64
			if err := rows.Scan(&id, &name, &managed, &created, &channels); err != nil {
				rows.Close()
				return err
			}
			uuid, err := gocql.ParseUUID(id)
			if err != nil || !utf8.ValidString(name) || utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 4000 || (managed != 0 && managed != 1) {
				rows.Close()
				return errors.New("invalid deletion group metadata")
			}
			groups = append(groups, deletionGroupCursor{id: uuid, created: created.UTC().Truncate(time.Millisecond)})
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		var total int64
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM user_group ug WHERE ug.team_id=$1::uuid`, team.String()).Scan(&total); err != nil {
			return err
		}
		return tx.Commit(ctx)
	})
	return groups, err
}

func (s userAuthStore) removeDeletionGroupMember(ctx context.Context, group, user gocql.UUID) error {
	return s.deletionGroupPostgres(ctx, func(conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, `DELETE FROM user_group_member WHERE user_group_id=$1::uuid AND user_id=$2::uuid`, group.String(), user.String())
		return err
	})
}
