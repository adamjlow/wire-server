// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/sync/errgroup"
)

var conversationMigrationColumns = []string{"type", "creator", "access", "access_role", "access_roles_v2", "name", "team", "deleted", "message_timer", "receipt_mode", "protocol", "group_id", "epoch", "writetime(epoch)", "cipher_suite", "group_conv_type", "channel_add_permission", "cells_state", "parent_conv", "history_depth"}
var conversationMemberColumns = []string{"user", "service", "provider", "status", "otr_muted_status", "otr_muted_ref", "otr_archived", "otr_archived_ref", "hidden", "hidden_ref", "conversation_role"}

const conversationMigrationSelect = `SELECT type,creator,access,access_role,access_roles_v2,name,team,deleted,message_timer,receipt_mode,protocol,group_id,epoch,WRITETIME(epoch),cipher_suite,group_conv_type,channel_add_permission,cells_state,parent_conv,history_depth FROM conversation WHERE conv=?`
const conversationMigrationMemberSelect = `SELECT user,service,provider,status,otr_muted_status,otr_muted_ref,otr_archived,otr_archived_ref,hidden,hidden_ref,conversation_role FROM member WHERE conv=?`
const conversationMigrationRemoteSelect = `SELECT user_remote_domain,user_remote_id,conversation_role FROM member_remote_user WHERE conv=?`
const conversationMigrationSubSelect = `SELECT subconv_id,cipher_suite,epoch,WRITETIME(epoch),group_id FROM subconversation WHERE conv_id=?`

type conversationMigrationQuery func(context.Context, string, []string, int, ...any) ([]migrationRow, error)

func conversationMigrationCassandraQuery(session *gocql.Session) conversationMigrationQuery {
	return func(ctx context.Context, cql string, columns []string, retries int, args ...any) ([]migrationRow, error) {
		iter := session.Query(cql, args...).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(cassandraRetry(retries)).Iter()
		var rows []migrationRow
		row := nullableCassandraRow(columns...)
		for iter.MapScan(row) {
			unwrapCassandraRow(row)
			rows = append(rows, migrationRow(row))
			row = nullableCassandraRow(columns...)
		}
		if err := iter.Close(); err != nil {
			return nil, errors.New("Cassandra conversation migration lookup failed")
		}
		return rows, nil
	}
}

// CQL decoding must precede toConv's missing-group skip and defaulting. Stored
// RoleName/Text fields have no fresh API grammar or length restriction.
func decodeConversationMigrationMember(row migrationRow) (conversationMigrationMember, bool, error) {
	invalid := errors.New("invalid stored conversation migration member")
	member := conversationMigrationMember{Role: "wire_admin"}
	for _, field := range []struct {
		name string
		out  *pgtype.UUID
	}{{"user", &member.User}, {"service", &member.Service}, {"provider", &member.Provider}} {
		id, ok := cqlUUID(row[field.name])
		if !ok || field.name == "user" && !id.Valid {
			return member, false, invalid
		}
		*field.out = id
	}
	status, err := userMigrationNullableInt32(row["status"])
	if err != nil {
		return member, false, invalid
	}
	member.Status.Muted, err = userMigrationNullableInt32(row["otr_muted_status"])
	if err != nil {
		return member, false, invalid
	}
	for _, field := range []struct {
		name string
		out  **string
	}{{"otr_muted_ref", &member.Status.MutedRef}, {"otr_archived_ref", &member.Status.ArchivedRef}, {"hidden_ref", &member.Status.HiddenRef}} {
		*field.out, err = userMigrationNullableString(row[field.name])
		if err != nil {
			return member, false, invalid
		}
	}
	for _, field := range []struct {
		name string
		out  *bool
	}{{"otr_archived", &member.Status.Archived}, {"hidden", &member.Status.Hidden}} {
		value, err := userMigrationNullableBool(row[field.name])
		if err != nil {
			return member, false, invalid
		}
		if value != nil {
			*field.out = *value
		}
	}
	role, err := userMigrationNullableString(row["conversation_role"])
	if err != nil {
		return member, false, invalid
	}
	if role != nil {
		member.Role = *role
	}
	if !member.Service.Valid || !member.Provider.Valid {
		member.Service, member.Provider = pgtype.UUID{}, pgtype.UUID{}
	}
	return member, status != nil && *status == 0, nil
}

func decodeConversationMigrationRemote(row migrationRow) (conversationMigrationRemoteMember, error) {
	invalid := errors.New("invalid stored remote conversation migration member")
	var member conversationMigrationRemoteMember
	domain, err := userMigrationString(row["user_remote_domain"])
	if err != nil || !validDomain(domain) {
		return member, invalid
	}
	member.Domain = strings.ToLower(domain)
	user, ok := cqlUUID(row["user_remote_id"])
	if !ok || !user.Valid {
		return member, invalid
	}
	member.User = user
	member.Role, err = userMigrationString(row["conversation_role"])
	if err != nil {
		return member, invalid
	}
	return member, nil
}

func normalizeConversationMigrationMetadata(row conversationMetadataRow) conversationMetadataRow {
	if len(row.Access) == 0 {
		row.Access = []int32{1}
		if row.Type == 0 {
			row.Access = []int32{2}
		}
	}
	if row.Type != 0 {
		row.AccessRoles = []int32{}
	} else if row.AccessRoles == nil {
		row.AccessRoles = []int32{1, 2, 4}
	} else {
		// Native access roles are a Set, unlike the access list.
		roles := make(map[int32]bool)
		for _, role := range row.AccessRoles {
			roles[role] = true
		}
		row.AccessRoles = make([]int32, 0, len(roles))
		for role := range roles {
			row.AccessRoles = append(row.AccessRoles, role)
		}
		sort.Slice(row.AccessRoles, func(i, j int) bool { return row.AccessRoles[i] < row.AccessRoles[j] })
	}
	if row.Protocol == nil || *row.Protocol == 0 {
		row.GroupID, row.Epoch, row.EpochTime, row.CipherSuite = nil, nil, nil, nil
	} else if row.Epoch == nil || row.EpochTime == nil || row.CipherSuite == nil {
		row.Epoch, row.EpochTime, row.CipherSuite = nil, nil, nil
	} else {
		cipher := int32(uint16(*row.CipherSuite))
		row.CipherSuite = &cipher
	}
	return row
}

func readConversationMigrationMembers(ctx context.Context, cid gocql.UUID, query conversationMigrationQuery) ([]conversationMigrationMember, error) {
	parents, err := query(ctx, `SELECT parent_conv FROM conversation WHERE conv=?`, []string{"parent_conv"}, 1, cid)
	if err != nil {
		return nil, err
	}
	ids := []gocql.UUID{cid}
	for _, row := range parents {
		parent, ok := cqlUUID(row["parent_conv"])
		if !ok {
			return nil, errors.New("invalid stored conversation migration parent")
		}
		if parent.Valid {
			ids = append(ids, gocql.UUID(parent.Bytes))
		}
	}
	parts := make([][]conversationMigrationMember, len(ids))
	group, attempt := errgroup.WithContext(ctx)
	group.SetLimit(16)
	for i, id := range ids {
		group.Go(func() error {
			rows, err := query(attempt, conversationMigrationMemberSelect, conversationMemberColumns, 1, id)
			if err != nil {
				return err
			}
			for _, row := range rows {
				member, active, err := decodeConversationMigrationMember(row)
				if err != nil {
					return err
				}
				if active {
					parts[i] = append(parts[i], member)
				}
			}
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}
	var members []conversationMigrationMember
	seen := make(map[[16]byte]bool)
	for _, part := range parts {
		for _, member := range part {
			if !seen[member.User.Bytes] {
				members = append(members, member)
				seen[member.User.Bytes] = true
			}
		}
	}
	return members, nil
}

func readConversationMigration(ctx context.Context, id pgtype.UUID, query conversationMigrationQuery,
	mls func(context.Context, []byte) ([]conversationMigrationMLSClient, error),
	deleteMarked func(context.Context) error,
) (*conversationMigrationRecord, error) {
	if !id.Valid {
		return nil, errors.New("invalid conversation migration identifier")
	}
	cid := gocql.UUID(id.Bytes)
	rows, err := query(ctx, conversationMigrationSelect, conversationMigrationColumns, 1, cid)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	metadata, deleted, err := decodeCassandraConversation(rows[0])
	if err != nil || metadata.Name != nil && !utf8.ValidString(*metadata.Name) {
		return nil, errors.New("invalid stored conversation migration metadata")
	}
	// The typed native tuple decodes the legacy field even when v2 wins.
	if legacy := rows[0]["access_role"]; legacy != nil {
		n, ok := cqlInteger(legacy)
		if !ok || n < 1 || n > 4 {
			return nil, errors.New("invalid stored conversation migration access role")
		}
	}
	if deleted {
		return nil, deleteMarked(ctx)
	}
	locals, err := readConversationMigrationMembers(ctx, cid, query)
	if err != nil {
		return nil, err
	}
	remoteRows, err := query(ctx, conversationMigrationRemoteSelect, []string{"user_remote_domain", "user_remote_id", "conversation_role"}, 1, cid)
	if err != nil {
		return nil, err
	}
	remotes := make([]conversationMigrationRemoteMember, len(remoteRows))
	for i, row := range remoteRows {
		remotes[i], err = decodeConversationMigrationRemote(row)
		if err != nil {
			return nil, err
		}
	}
	if metadata.Protocol != nil && *metadata.Protocol != 0 && metadata.GroupID == nil {
		return nil, nil
	}
	record := &conversationMigrationRecord{ID: id, Metadata: normalizeConversationMigrationMetadata(metadata), Members: locals, Remotes: remotes}
	subRows, err := query(ctx, conversationMigrationSubSelect, []string{"subconv_id", "cipher_suite", "epoch", "writetime(epoch)", "group_id"}, 1, cid)
	if err != nil {
		return nil, err
	}
	subs := make(map[string]cassandraSubConversation)
	for _, row := range subRows {
		sub, err := decodeCassandraSubConversation(row)
		if err != nil || !utf8.ValidString(sub.ID) {
			return nil, errors.New("invalid stored migration subconversation")
		}
		subs[sub.ID] = sub
	}
	groupState := func(sub *string) ([]byte, error) {
		cql, retries, args := `SELECT public_group_state FROM conversation WHERE conv=?`, 1, []any{cid}
		if sub != nil {
			cql, retries, args = `SELECT public_group_state FROM subconversation WHERE conv_id=? AND subconv_id=?`, 5, []any{cid, *sub}
		}
		rows, err := query(ctx, cql, []string{"public_group_state"}, retries, args...)
		if err != nil || len(rows) == 0 || rows[0]["public_group_state"] == nil {
			return nil, err
		}
		state, ok := rows[0]["public_group_state"].([]byte)
		if !ok {
			return nil, errors.New("invalid stored migration group state")
		}
		return state, nil
	}
	mainState, err := groupState(nil) // Native also reads this for Proteus.
	if err != nil {
		return nil, err
	}
	if record.Metadata.Protocol != nil && *record.Metadata.Protocol != 0 {
		clients, err := mls(ctx, record.Metadata.GroupID)
		if err != nil {
			return nil, err
		}
		if mainState != nil {
			record.GroupState, record.MLSClients = mainState, clients
		}
	}
	keys := make([]string, 0, len(subs))
	for key := range subs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		sub := subs[key]
		clients, err := mls(ctx, sub.GroupID)
		if err != nil {
			return nil, err
		}
		state, err := groupState(&sub.ID)
		if err != nil {
			return nil, err
		}
		migrated := conversationMigrationSub{ID: sub.ID, GroupID: sub.GroupID, GroupState: state, EpochTime: time.Unix(0, 0).UTC()}
		if sub.Epoch != 0 {
			cipher := int32(uint16(sub.CipherSuite))
			migrated.CipherSuite, migrated.Epoch, migrated.EpochTime = &cipher, sub.Epoch, time.UnixMicro(sub.EpochWriteTime).UTC()
		}
		record.Subs = append(record.Subs, migrated)
		record.MLSClients = append(record.MLSClients, clients...)
	}
	return record, nil
}
