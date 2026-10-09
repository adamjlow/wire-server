// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/jackc/pgx/v5/pgtype"
)

type cassandraConversationStore struct{ Session *gocql.Session }

func cassandraRetry(attempts int) gocql.RetryPolicy {
	return cassandraSourceRetry{attempts: attempts}
}

type cassandraSourceRetry struct{ attempts int }

func (r cassandraSourceRetry) Attempt(q gocql.RetryableQuery) bool {
	if q.Attempts() > r.attempts || q.Context().Err() != nil {
		return false
	}
	if r.attempts == 1 {
		return true // Source x1 is immediate.
	}
	// Source x5 uses100,200,400,800,1600ms, without gocql's random jitter.
	return wait(q.Context(), (100*time.Millisecond)<<uint(min(q.Attempts()-1, 4)))
}

func (cassandraSourceRetry) GetRetryType(error) gocql.RetryType { return gocql.RetryNextHost }

func (s cassandraConversationStore) query(ctx context.Context, cql string, args ...any) *gocql.Query {
	return s.Session.Query(cql, args...).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(cassandraRetry(1))
}

func cqlInteger(value any) (int64, bool) {
	switch v := value.(type) {
	case int:
		return int64(v), true
	case int32:
		return int64(v), true
	case int64:
		return v, true
	default:
		return 0, false
	}
}

func cqlUUID(value any) (pgtype.UUID, bool) {
	if value == nil {
		return pgtype.UUID{}, true
	}
	u, ok := value.(gocql.UUID)
	return pgtype.UUID{Bytes: [16]byte(u), Valid: ok}, ok
}

func decodeCassandraConversation(values map[string]any) (conversationMetadataRow, bool, error) {
	invalid := errors.New("invalid Cassandra conversation metadata")
	var row conversationMetadataRow
	typ, ok := cqlInteger(values["type"])
	if !ok || typ < 0 || typ > 3 {
		return row, false, invalid
	}
	row.Type = int32(typ)
	for _, f := range []struct {
		name string
		out  *pgtype.UUID
	}{{"creator", &row.Creator}, {"team", &row.Team}, {"parent_conv", &row.Parent}} {
		*f.out, ok = cqlUUID(values[f.name])
		if !ok {
			return row, false, invalid
		}
	}
	for _, f := range []struct {
		name string
		out  **int32
	}{{"receipt_mode", &row.ReceiptMode}, {"protocol", &row.Protocol}, {"cipher_suite", &row.CipherSuite}, {"group_conv_type", &row.GroupType}, {"channel_add_permission", &row.AddPermission}, {"cells_state", &row.CellsState}} {
		if values[f.name] != nil {
			n, valid := cqlInteger(values[f.name])
			if !valid || n < math.MinInt32 || n > math.MaxInt32 {
				return row, false, invalid
			}
			v := int32(n)
			*f.out = &v
		}
	}
	for _, f := range []struct {
		name string
		out  **int64
	}{{"message_timer", &row.MessageTimer}, {"epoch", &row.Epoch}, {"history_depth", &row.HistoryDepth}} {
		if values[f.name] != nil {
			n, valid := cqlInteger(values[f.name])
			if !valid || f.name == "history_depth" && n < 0 {
				return row, false, invalid
			}
			*f.out = &n
		}
	}
	for _, f := range []struct {
		name string
		out  *[]int32
	}{{"access", &row.Access}, {"access_roles_v2", &row.AccessRoles}} {
		switch v := values[f.name].(type) {
		case nil:
		case []int:
			*f.out = make([]int32, len(v))
			for i, n := range v {
				if n < 1 || n > 4 {
					return row, false, invalid
				}
				(*f.out)[i] = int32(n)
			}
		case []int32:
			*f.out = v
		default:
			return row, false, invalid
		}
	}
	if row.AccessRoles == nil && values["access_role"] != nil {
		role, valid := cqlInteger(values["access_role"])
		if !valid || role < 1 || role > 4 {
			return row, false, invalid
		}
		// CQL AccessRoleLegacy uses1-4, unlike the enum's0-based constructors.
		row.AccessRoles = [][]int32{{}, {1}, {1, 2, 4}, {1, 2, 3, 4}}[role-1]
	}
	if values["name"] != nil {
		name, valid := values["name"].(string)
		if !valid {
			return row, false, invalid
		}
		row.Name = &name
	}
	if values["group_id"] != nil {
		row.GroupID, ok = values["group_id"].([]byte)
		if !ok {
			return row, false, invalid
		}
	}
	if values["writetime(epoch)"] != nil {
		micros, valid := cqlInteger(values["writetime(epoch)"])
		if !valid {
			return row, false, invalid
		}
		tm := time.UnixMicro(micros).UTC()
		row.EpochTime = &tm
	}
	deleted := false
	if values["deleted"] != nil {
		deleted, ok = values["deleted"].(bool)
		if !ok {
			return row, false, invalid
		}
	}
	if !row.valid() {
		return row, false, invalid
	}
	return row, deleted, nil
}

func (s cassandraConversationStore) Conversation(ctx context.Context, id string) (*storedConversation, error) {
	cid, err := parseUserID(id)
	if err != nil {
		return nil, errors.New("invalid Cassandra conversation identifier")
	}
	values := nullableCassandraRow("type", "creator", "access", "access_role", "access_roles_v2", "name", "team", "deleted", "message_timer", "receipt_mode", "protocol", "group_id", "epoch", "writetime(epoch)", "cipher_suite", "group_conv_type", "channel_add_permission", "cells_state", "parent_conv", "history_depth")
	err = s.query(ctx, `SELECT type,creator,access,access_role,access_roles_v2,name,team,deleted,message_timer,receipt_mode,protocol,group_id,epoch,WRITETIME(epoch),cipher_suite,group_conv_type,channel_add_permission,cells_state,parent_conv,history_depth FROM conversation WHERE conv=?`, cid).MapScan(values)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.New("Cassandra conversation query failed")
	}
	unwrapCassandraRow(values)
	row, deleted, err := decodeCassandraConversation(values)
	if err != nil {
		return nil, err
	}
	if deleted {
		if err = s.DeleteConversation(ctx, id); err != nil {
			return nil, err
		}
		return nil, nil
	}
	locals, err := s.localMembers(ctx, cid)
	if err != nil {
		return nil, err
	}
	remotes, err := s.remoteMembers(ctx, cid)
	if err != nil {
		return nil, err
	}
	if row.Protocol != nil && *row.Protocol != 0 && row.GroupID == nil {
		return nil, nil
	}
	conv := &storedConversation{ID: cid.String(), Metadata: row.values(), LocalMembers: locals, RemoteMembers: remotes}
	if row.Team.Valid {
		team := postgresUUIDString(row.Team)
		conv.Team = &team
	}
	return conv, nil
}

func (s cassandraConversationStore) localMembers(ctx context.Context, cid gocql.UUID) ([]localConversationMember, error) {
	var parent *gocql.UUID
	err := s.query(ctx, `SELECT parent_conv FROM conversation WHERE conv=?`, cid).Scan(&parent)
	if err != nil && !errors.Is(err, gocql.ErrNotFound) {
		return nil, errors.New("Cassandra conversation parent query failed")
	}
	ids := []gocql.UUID{cid}
	if parent != nil {
		ids = append(ids, *parent)
	}
	members := []localConversationMember{}
	seen := make(map[string]bool)
	for _, id := range ids {
		iter := s.query(ctx, `SELECT user,service,provider,status,otr_muted_status,otr_muted_ref,otr_archived,otr_archived_ref,hidden,hidden_ref,conversation_role FROM member WHERE conv=?`, id).Iter()
		values := nullableCassandraRow("user", "service", "provider", "status", "otr_muted_status", "otr_muted_ref", "otr_archived", "otr_archived_ref", "hidden", "hidden_ref", "conversation_role")
		for iter.MapScan(values) {
			unwrapCassandraRow(values)
			status, ok := cqlInteger(values["status"])
			if ok && status == 0 {
				user, valid := cqlUUID(values["user"])
				if !valid || !user.Valid {
					_ = iter.Close()
					return nil, errors.New("invalid Cassandra conversation member")
				}
				uid := postgresUUIDString(user)
				if !seen[uid] {
					member := localConversationMember{ID: uid, Role: "wire_admin"}
					if values["conversation_role"] != nil {
						member.Role, valid = values["conversation_role"].(string)
						if !valid || !validConversationRole(member.Role) {
							_ = iter.Close()
							return nil, errors.New("invalid Cassandra member role")
						}
					}
					service, serviceOK := cqlUUID(values["service"])
					provider, providerOK := cqlUUID(values["provider"])
					if !serviceOK || !providerOK {
						_ = iter.Close()
						return nil, errors.New("invalid Cassandra bot service")
					}
					if service.Valid && provider.Valid {
						member.Service, member.Provider = stringPtr(postgresUUIDString(service)), stringPtr(postgresUUIDString(provider))
					}
					members = append(members, member)
					seen[uid] = true
				}
			}
			values = nullableCassandraRow("user", "service", "provider", "status", "otr_muted_status", "otr_muted_ref", "otr_archived", "otr_archived_ref", "hidden", "hidden_ref", "conversation_role")
		}
		if iter.Close() != nil {
			return nil, errors.New("Cassandra member query failed")
		}
	}
	return members, nil
}

func (s cassandraConversationStore) remoteMembers(ctx context.Context, cid gocql.UUID) ([]remoteConversationMember, error) {
	iter := s.query(ctx, `SELECT user_remote_domain,user_remote_id,conversation_role FROM member_remote_user WHERE conv=?`, cid).Iter()
	members := []remoteConversationMember{}
	var domain, role string
	var user gocql.UUID
	for iter.Scan(&domain, &user, &role) {
		if !validDomain(domain) || !validConversationRole(role) {
			_ = iter.Close()
			return nil, errors.New("invalid Cassandra remote member")
		}
		members = append(members, remoteConversationMember{User: qualifiedUser{ID: user.String(), Domain: strings.ToLower(domain)}, Role: role})
	}
	if iter.Close() != nil {
		return nil, errors.New("Cassandra remote member query failed")
	}
	return members, nil
}

func (s cassandraConversationStore) UpsertLocalMembers(ctx context.Context, conv string, users []string, role string) error {
	cid, err := parseUserID(conv)
	if err != nil || !validConversationRole(role) {
		return errors.New("invalid Cassandra member update")
	}
	ids := make([]gocql.UUID, len(users))
	for i, id := range users {
		ids[i], err = parseUserID(id)
		if err != nil {
			return errors.New("invalid Cassandra member identifier")
		}
	}
	for start := 0; start < len(ids); start += 32 {
		batch := s.Session.NewBatch(gocql.LoggedBatch).WithContext(ctx)
		batch.Cons = gocql.LocalQuorum
		batch.RetryPolicy(cassandraRetry(5))
		for _, uid := range ids[start:min(start+32, len(ids))] {
			// Source writes explicit null service/provider and active status0,
			// including when this upsert replaces an existing member row.
			batch.Query(`INSERT INTO member (conv,user,service,provider,status,conversation_role) VALUES (?,?,?,?,0,?)`, cid, uid, nil, nil, role)
			batch.Query(`INSERT INTO user (user,conv) VALUES (?,?)`, uid, cid)
		}
		if s.Session.ExecuteBatch(batch) != nil {
			return errors.New("Cassandra member batch failed")
		}
	}
	return nil
}

func (s cassandraConversationStore) DeleteConversation(ctx context.Context, id string) error {
	cid, err := parseUserID(id)
	if err != nil {
		return errors.New("invalid Cassandra conversation identifier")
	}
	if err = s.query(ctx, `UPDATE conversation SET deleted=true WHERE conv=?`, cid).RetryPolicy(cassandraRetry(5)).Exec(); err != nil {
		return errors.New("Cassandra conversation deletion mark failed")
	}
	locals, err := s.localMembers(ctx, cid)
	if err != nil {
		return err
	}
	remotes, err := s.remoteMembers(ctx, cid)
	if err != nil {
		return err
	}
	batch := s.Session.NewBatch(gocql.LoggedBatch).WithContext(ctx)
	batch.Cons = gocql.LocalQuorum
	batch.RetryPolicy(cassandraRetry(5))
	for _, user := range locals {
		uid, _ := parseUserID(user.ID)
		batch.Query(`DELETE FROM member WHERE conv=? AND user=?`, cid, uid)
		batch.Query(`DELETE FROM user WHERE user=? AND conv=?`, uid, cid)
	}
	if len(batch.Entries) > 0 && s.Session.ExecuteBatch(batch) != nil {
		return errors.New("Cassandra local member deletion failed")
	}
	batch = s.Session.NewBatch(gocql.LoggedBatch).WithContext(ctx)
	batch.Cons = gocql.LocalQuorum
	batch.RetryPolicy(cassandraRetry(5))
	for _, user := range remotes {
		uid, _ := parseUserID(user.User.ID)
		batch.Query(`DELETE FROM member_remote_user WHERE conv=? AND user_remote_domain=? AND user_remote_id=?`, cid, user.User.Domain, uid)
	}
	if len(batch.Entries) > 0 && s.Session.ExecuteBatch(batch) != nil {
		return errors.New("Cassandra remote member deletion failed")
	}
	// Source uses a year3000 tombstone to prevent stale writes resurrecting
	// migrated/deleted conversations. Do not replace with the current timestamp.
	if err = s.query(ctx, `DELETE FROM conversation USING TIMESTAMP 32503680000000000 WHERE conv=?`, cid).RetryPolicy(cassandraRetry(5)).Exec(); err != nil {
		return errors.New("Cassandra conversation deletion failed")
	}
	return nil
}
