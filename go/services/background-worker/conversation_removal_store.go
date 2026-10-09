// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"sort"
	"time"
	"unicode/utf8"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type scheduledRemovalStore struct {
	Postgres        *postgresStore
	Cassandra       cassandraConversationStore
	Location, Codes storageLocation
}

// Source routes a mutation under shared lock+pending cleanup, then either by
// EXISTS or to both stores. Do not keep one lock around the whole subsystem:
// that changes operation boundaries and can deadlock nested migration cleanup.
func (s scheduledRemovalStore) route(ctx context.Context, id string, both bool, cass, pg func() error) error {
	switch s.Location {
	case storageCassandra:
		return cass()
	case storagePostgres:
		return pg()
	case storageMigrating:
		if s.Postgres == nil {
			return errors.New("conversation removal Postgres unavailable")
		}
		return s.Postgres.WithConversationLock(ctx, id, false, func() error {
			if err := s.Postgres.CleanupConversation(ctx, id); err != nil {
				return err
			}
			if both {
				if err := cass(); err != nil {
					return err
				}
				return pg()
			}
			exists, err := s.Postgres.ConversationExists(ctx, id)
			if err != nil {
				return err
			}
			if exists {
				return pg()
			}
			return cass()
		})
	default:
		return errors.New("invalid conversation removal storage")
	}
}
func (s scheduledRemovalStore) pgExec(ctx context.Context, sql string, args ...any) error {
	if s.Postgres == nil {
		return errors.New("conversation removal Postgres unavailable")
	}
	return retryPostgres(ctx, s.Postgres.Pool.Reset, func() error {
		conn, err := s.Postgres.acquire(ctx)
		if err != nil {
			return err
		}
		defer conn.Release()
		if _, err := conn.Exec(ctx, sql, args...); err != nil {
			return postgresFailure(err, "conversation removal write failed")
		}
		return nil
	})
}
func (s scheduledRemovalStore) cassExec(ctx context.Context, cql string, args ...any) error {
	if s.Cassandra.Session == nil {
		return errors.New("conversation removal Cassandra unavailable")
	}
	if err := s.Cassandra.query(ctx, cql, args...).RetryPolicy(cassandraRetry(5)).Exec(); err != nil {
		return errors.New("conversation removal Cassandra write failed")
	}
	return nil
}
func (s scheduledRemovalStore) RemoveMLSClients(ctx context.Context, group []byte) error {
	var id string
	if s.Location == storageMigrating {
		var err error
		id, err = removalGroupConversation(group)
		if err != nil {
			return err
		}
	}
	return s.route(ctx, id, true, func() error { return s.cassExec(ctx, `DELETE FROM mls_group_member_client WHERE group_id=?`, group) }, func() error {
		return s.pgExec(ctx, `DELETE FROM mls_group_member_client WHERE group_id=$1::bytea`, group)
	})
}
func (s scheduledRemovalStore) RemoveHistoryClients(ctx context.Context, group []byte) error {
	// History is Postgres-owned in every conversation storage mode.
	return s.pgExec(ctx, `DELETE FROM mls_history_client WHERE group_id=$1::bytea`, group)
}
func (s scheduledRemovalStore) DeleteProposals(ctx context.Context, group []byte) error {
	// ProposalStore always uses Cassandra, independent of conversation mode.
	return s.cassExec(ctx, `DELETE FROM mls_proposal_refs WHERE group_id=?`, group)
}
func (s scheduledRemovalStore) DeleteSubConversation(ctx context.Context, id, sub string) error {
	cid, err := postgresUUID(id)
	if err != nil {
		return err
	}
	return s.route(ctx, id, false, func() error {
		return s.cassExec(ctx, `DELETE FROM subconversation WHERE conv_id=? AND subconv_id=?`, gocql.UUID(cid.Bytes), sub)
	}, func() error {
		return s.pgExec(ctx, `DELETE FROM subconversation WHERE conv_id=$1::uuid AND subconv_id=$2::text`, cid, sub)
	})
}
func (s scheduledRemovalStore) SubConversations(ctx context.Context, id string) (result []removalSubConversation, err error) {
	cid, err := postgresUUID(id)
	if err != nil {
		return nil, err
	}
	var byID map[string][]byte
	err = s.route(ctx, id, false, func() error {
		if s.Cassandra.Session == nil {
			return errors.New("conversation removal Cassandra unavailable")
		}
		rows, err := conversationMigrationCassandraQuery(s.Cassandra.Session)(ctx, conversationMigrationSubSelect, []string{"subconv_id", "cipher_suite", "epoch", "writetime(epoch)", "group_id"}, 1, gocql.UUID(cid.Bytes))
		if err != nil {
			return err
		}
		byID = make(map[string][]byte)
		for _, row := range rows {
			sub, err := decodeCassandraSubConversation(row)
			if err != nil {
				return err
			}
			byID[sub.ID] = sub.GroupID
		}
		return nil
	}, func() error {
		if s.Postgres == nil {
			return errors.New("conversation removal Postgres unavailable")
		}
		return retryPostgres(ctx, s.Postgres.Pool.Reset, func() error {
			byID = make(map[string][]byte)
			conn, err := s.Postgres.acquire(ctx)
			if err != nil {
				return err
			}
			defer conn.Release()
			rows, err := conn.Query(ctx, `SELECT subconv_id,cipher_suite,epoch,epoch_timestamp,group_id FROM subconversation WHERE conv_id=$1::uuid`, cid)
			if err != nil {
				return postgresFailure(err, "removal subconversation read failed")
			}
			defer rows.Close()
			for rows.Next() {
				var sub string
				var cipher *int32
				var epoch int64
				var stamp time.Time
				var group []byte
				if err := rows.Scan(&sub, &cipher, &epoch, &stamp, &group); err != nil {
					return postgresFailure(err, "removal subconversation decode failed")
				}
				if !utf8.ValidString(sub) || group == nil {
					return errors.New("invalid removal subconversation")
				}
				if cipher != nil {
					switch uint16(*cipher) {
					case 1, 2, 5, 7:
					default:
						return errors.New("invalid removal subconversation cipher")
					}
				}
				byID[sub] = group
			}
			if err := rows.Err(); err != nil {
				return postgresFailure(err, "removal subconversation read failed")
			}
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	for sub, group := range byID {
		result = append(result, removalSubConversation{ID: sub, Group: group})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return
}
func (s scheduledRemovalStore) DeleteConversationCode(ctx context.Context, id string) error {
	cid, err := parseUserID(id)
	if err != nil {
		return errors.New("invalid conversation code identifier")
	}
	hash := sha256.Sum256([]byte(cid.String()))
	key := base64.RawURLEncoding.EncodeToString(hash[:15])
	cass := func() error { return s.cassExec(ctx, `DELETE FROM conversation_codes WHERE key=? AND scope=1`, key) }
	pg := func() error { return s.pgExec(ctx, `DELETE FROM conversation_codes WHERE key=$1::text`, key) }
	switch s.Codes {
	case storageCassandra:
		return cass()
	case storagePostgres:
		return pg()
	case storageMigrating:
		if err := cass(); err != nil {
			return err
		}
		return pg()
	default:
		return errors.New("invalid conversation code storage")
	}
}
func (s scheduledRemovalStore) DeleteConversationState(ctx context.Context, id string, team *string) error {
	cid, err := postgresUUID(id)
	if err != nil {
		return err
	}
	cass := func() error {
		if s.Cassandra.Session == nil {
			return errors.New("conversation removal Cassandra unavailable")
		}
		if team != nil {
			tid, err := parseUserID(*team)
			if err != nil {
				return errors.New("invalid removal team identifier")
			}
			batch := s.Cassandra.Session.NewBatch(gocql.LoggedBatch).WithContext(ctx)
			batch.Cons = gocql.LocalQuorum
			batch.RetryPolicy(cassandraRetry(5))
			batch.Query(`UPDATE conversation SET deleted=true WHERE conv=?`, gocql.UUID(cid.Bytes))
			batch.Query(`DELETE FROM team_conv WHERE team=? AND conv=?`, tid, gocql.UUID(cid.Bytes))
			if err := s.Cassandra.Session.ExecuteBatch(batch); err != nil {
				return errors.New("conversation removal team batch failed")
			}
		}
		return s.Cassandra.DeleteConversation(ctx, id)
	}
	return s.route(ctx, id, true, cass, func() error { return s.pgExec(ctx, `DELETE FROM conversation WHERE id=$1::uuid`, cid) })
}
