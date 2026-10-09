// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"errors"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/jackc/pgx/v5/pgtype"
)

type adminlessConversationStore struct {
	Read          conversationStore
	Data          scheduledRemovalStore
	Domain        string
	FeatureLookup func(context.Context, string) (adminlessFeature, error)
	UserLookup    func(context.Context, []string) ([]adminlessUser, error)
}

func (s adminlessConversationStore) Conversation(ctx context.Context, id string) (*storedConversation, error) {
	if s.Read == nil {
		return nil, errors.New("adminless reader unavailable")
	}
	return s.Read.Conversation(ctx, id)
}
func (s adminlessConversationStore) Feature(ctx context.Context, team string) (adminlessFeature, error) {
	if s.FeatureLookup == nil {
		return adminlessFeature{}, errors.New("adminless features unavailable")
	}
	return s.FeatureLookup(ctx, team)
}
func (s adminlessConversationStore) Users(ctx context.Context, ids []string) ([]adminlessUser, error) {
	if s.UserLookup == nil {
		return nil, errors.New("adminless accounts unavailable")
	}
	return s.UserLookup(ctx, ids)
}
func (s adminlessConversationStore) TeamConversations(ctx context.Context, team string) ([]string, error) {
	tid, err := postgresUUID(team)
	if err != nil {
		return nil, err
	}
	cass := func() ([]string, error) {
		if s.Data.Cassandra.Session == nil {
			return nil, errors.New("adminless Cassandra unavailable")
		}
		iter := s.Data.Cassandra.query(ctx, `SELECT conv FROM team_conv WHERE team=? ORDER BY conv`, gocql.UUID(tid.Bytes)).Iter()
		var ids []string
		var id gocql.UUID
		for iter.Scan(&id) {
			ids = append(ids, id.String())
		}
		if err := iter.Close(); err != nil {
			return nil, errors.New("adminless team read failed")
		}
		return ids, nil
	}
	pg := func() (ids []string, err error) {
		p := s.Data.Postgres
		if p == nil {
			return nil, errors.New("adminless Postgres unavailable")
		}
		err = retryPostgres(ctx, p.Pool.Reset, func() error {
			ids = nil
			conn, err := p.acquire(ctx)
			if err != nil {
				return err
			}
			defer conn.Release()
			rows, err := conn.Query(ctx, `SELECT id FROM conversation WHERE team=$1::uuid`, tid)
			if err != nil {
				return postgresFailure(err, "adminless team read failed")
			}
			defer rows.Close()
			for rows.Next() {
				var id pgtype.UUID
				if err := rows.Scan(&id); err != nil {
					return postgresFailure(err, "adminless team decode failed")
				}
				if !id.Valid {
					return errors.New("invalid adminless team conversation")
				}
				ids = append(ids, postgresUUIDString(id))
			}
			if err := rows.Err(); err != nil {
				return postgresFailure(err, "adminless team read failed")
			}
			return nil
		})
		return
	}
	switch s.Data.Location {
	case storageCassandra:
		return cass()
	case storagePostgres:
		return pg()
	case storageMigrating:
		// Native scans Cassandra first, then PG, returns stable nubOrd(PG<>Cass).
		// This aggregate deliberately has no individual-conversation lock.
		c, err := cass()
		if err != nil {
			return nil, err
		}
		p, err := pg()
		if err != nil {
			return nil, err
		}
		seen := map[string]bool{}
		var ids []string
		for _, id := range append(p, c...) {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
		return ids, nil
	default:
		return nil, errors.New("invalid adminless storage location")
	}
}
func (s adminlessConversationStore) Promote(ctx context.Context, conv *storedConversation, user qualifiedUser) error {
	if conv == nil || !validDomain(s.Domain) || user.Domain != s.Domain {
		return errors.New("invalid local adminless promotion")
	}
	cid, err := postgresUUID(conv.ID)
	if err != nil {
		return err
	}
	uid, err := postgresUUID(user.ID)
	if err != nil {
		return err
	}
	return s.Data.route(ctx, conv.ID, false, func() error {
		session := s.Data.Cassandra.Session
		if session == nil {
			return errors.New("adminless Cassandra unavailable")
		}
		// Source uses an unlogged batch even for this single optional-role update.
		batch := session.NewBatch(gocql.UnloggedBatch).WithContext(ctx)
		batch.Cons = gocql.LocalQuorum
		batch.RetryPolicy(cassandraRetry(5))
		batch.Query(`UPDATE member SET conversation_role=? WHERE conv=? AND user=?`, "wire_admin", gocql.UUID(cid.Bytes), gocql.UUID(uid.Bytes))
		if err := session.ExecuteBatch(batch); err != nil {
			return errors.New("adminless promotion failed")
		}
		return nil
	}, func() error {
		return s.Data.pgExec(ctx, `UPDATE conversation_member SET conversation_role=$3::text WHERE conv=$1::uuid AND "user"=$2::uuid`, cid, uid, "wire_admin")
	})
}
