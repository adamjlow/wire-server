// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/jackc/pgx/v5"
)

// Native migration pagination reads Cassandra first, then PostgreSQL. On a
// short final Cassandra page it appends a full PostgreSQL page, so a combined
// page may contain up to199 bots. Preserve that read-before-effects boundary.
func (s userStore) serviceBotPages(ctx context.Context, provider, service gocql.UUID, visit func([]deletionServiceBot) error) error {
	if s.mode != "cassandra" && s.mode != "postgresql" && s.mode != "migration-to-postgresql" {
		return errors.New("invalid user storage mode")
	}
	var marker *gocql.UUID
	if s.mode != "postgresql" {
		var state []byte
		for {
			bots, next, err := s.serviceBotsCassandra(ctx, provider, service, state)
			if err != nil {
				return err
			}
			if len(next) == 0 && s.mode == "migration-to-postgresql" && len(bots) < 100 {
				pgBots, err := s.serviceBotsPostgres(ctx, provider, service, nil)
				if err != nil {
					return err
				}
				bots = append(bots, pgBots...)
				if len(pgBots) > 0 {
					last := pgBots[len(pgBots)-1].user
					marker = &last
				} else {
					return visit(bots)
				}
			}
			if err := visit(bots); err != nil {
				return err
			}
			if len(next) == 0 {
				break
			}
			state = next
		}
		if s.mode == "cassandra" {
			return nil
		}
		if marker == nil {
			zero := gocql.UUID{}
			marker = &zero
		}
	}
	for {
		bots, err := s.serviceBotsPostgres(ctx, provider, service, marker)
		if err != nil {
			return err
		}
		if len(bots) == 0 {
			return nil
		}
		if err := visit(bots); err != nil {
			return err
		}
		last := bots[len(bots)-1].user
		marker = &last
	}
}

func (s userStore) serviceBotsCassandra(ctx context.Context, provider, service gocql.UUID, state []byte) ([]deletionServiceBot, []byte, error) {
	iter := s.cassandra.Query(`SELECT user,conv,team FROM service_user WHERE provider=? AND service=?`, provider, service).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).PageSize(100).PageState(state).Iter()
	var bots []deletionServiceBot
	var user, conv, team *gocql.UUID
	for iter.Scan(&user, &conv, &team) {
		if user == nil || conv == nil {
			_ = iter.Close()
			return nil, nil, errors.New("invalid service bot row")
		}
		bots = append(bots, deletionServiceBot{user: *user, conversation: *conv})
	}
	state = append([]byte(nil), iter.PageState()...)
	return bots, state, userStorageError(iter.Close())
}

func (s userStore) serviceBotsPostgres(ctx context.Context, provider, service gocql.UUID, marker *gocql.UUID) ([]deletionServiceBot, error) {
	var bots []deletionServiceBot
	err := s.withPostgres(ctx, func(conn *pgx.Conn) error {
		query := `SELECT b.id::text,b.conv::text,b.conv_team::text FROM bot_conv b JOIN wire_user u ON b.id=u.id WHERE u.provider=$1::uuid AND u.service=$2::uuid`
		args := []any{provider.String(), service.String()}
		if marker != nil {
			query += ` AND b.id>$3::uuid`
			args = append(args, marker.String())
		}
		rows, err := conn.Query(ctx, query+` ORDER BY b.id LIMIT 100`, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var user, conv string
			var team *string
			if err := rows.Scan(&user, &conv, &team); err != nil {
				return err
			}
			uid, err := gocql.ParseUUID(user)
			if err != nil {
				return err
			}
			cid, err := gocql.ParseUUID(conv)
			if err != nil {
				return err
			}
			bots = append(bots, deletionServiceBot{user: uid, conversation: cid})
		}
		return rows.Err()
	})
	return bots, userStorageError(err)
}

func (s userStore) deleteServiceBot(ctx context.Context, provider, service, user gocql.UUID) error {
	return s.withUserBackend(ctx, user, func(postgres bool) error {
		if postgres {
			return userStorageError(s.withPostgres(ctx, func(conn *pgx.Conn) error {
				_, err := conn.Exec(ctx, `DELETE FROM bot_conv WHERE id=$1::uuid`, user.String())
				return err
			}))
		}
		var conv, team *gocql.UUID
		err := s.cassandra.Query(`SELECT conv,team FROM service_user WHERE provider=? AND service=? AND user=?`, provider, service, user).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).Scan(&conv, &team)
		if errors.Is(err, gocql.ErrNotFound) {
			return nil
		}
		if err != nil {
			return userStorageError(err)
		}
		if conv == nil {
			return errors.New("invalid service bot row")
		}
		batch := s.cassandra.NewBatch(gocql.LoggedBatch).WithContext(ctx)
		batch.Cons = gocql.LocalQuorum
		batch.RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5})
		batch.Query(`DELETE FROM service_user WHERE provider=? AND service=? AND user=?`, provider, service, user)
		if team != nil {
			batch.Query(`DELETE FROM service_team WHERE provider=? AND service=? AND team=? AND user=?`, provider, service, *team, user)
		}
		return userStorageError(s.cassandra.ExecuteBatch(batch))
	})
}
