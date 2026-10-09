// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type conversationMetadataRow struct {
	Type                                 int32
	Creator, Team, Parent                pgtype.UUID
	Access, AccessRoles                  []int32
	Name                                 *string
	MessageTimer, Epoch, HistoryDepth    *int64
	ReceiptMode, Protocol, CipherSuite   *int32
	GroupType, AddPermission, CellsState *int32
	GroupID                              []byte
	EpochTime                            *time.Time
}

func validConversationRole(role string) bool {
	if len(role) < 2 || len(role) > 128 {
		return false
	}
	for _, c := range role {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}

func (r conversationMetadataRow) valid() bool {
	if r.Type < 0 || r.Type > 3 {
		return false
	}
	for _, values := range [][]int32{r.Access, r.AccessRoles} {
		for _, v := range values {
			if v < 1 || v > 4 {
				return false
			}
		}
	}
	for _, bounded := range []struct {
		value *int32
		max   int32
	}{{r.Protocol, 2}, {r.CellsState, 2}, {r.GroupType, 2}, {r.AddPermission, 1}} {
		if bounded.value != nil && (*bounded.value < 0 || *bounded.value > bounded.max) {
			return false
		}
	}
	if r.CipherSuite != nil {
		// Source casts to Word16 before resolving its four supported suites.
		switch uint16(*r.CipherSuite) {
		case 1, 2, 5, 7:
		default:
			return false
		}
	}
	return true
}

func nullableUUID(id pgtype.UUID) any {
	if !id.Valid {
		return nil
	}
	return postgresUUIDString(id)
}

func (r conversationMetadataRow) values() map[string]any {
	protocol, cells := int32(0), int32(0)
	if r.Protocol != nil {
		protocol = *r.Protocol
	}
	if r.CellsState != nil {
		cells = *r.CellsState
	}
	return map[string]any{"type": r.Type, "creator": nullableUUID(r.Creator), "access": r.Access, "access_roles_v2": r.AccessRoles, "name": r.Name, "team": nullableUUID(r.Team), "message_timer": r.MessageTimer, "receipt_mode": r.ReceiptMode, "protocol": protocol, "group_id": r.GroupID, "epoch": r.Epoch, "epoch_timestamp": r.EpochTime, "cipher_suite": r.CipherSuite, "group_conv_type": r.GroupType, "channel_add_permission": r.AddPermission, "cells_state": cells, "parent_conv": nullableUUID(r.Parent), "history_depth": r.HistoryDepth}
}

func (s *postgresStore) conversationOnce(ctx context.Context, id string) (*storedConversation, error) {
	cid, err := postgresUUID(id)
	if err != nil {
		return nil, err
	}
	conn, err := s.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, postgresFailure(err, "conversation read transaction failed")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var row conversationMetadataRow
	err = tx.QueryRow(ctx, `SELECT type,creator,access,access_roles_v2,name,team,message_timer,receipt_mode,protocol,group_id,epoch,epoch_timestamp,cipher_suite,group_conv_type,channel_add_permission,cells_state,parent_conv,history_depth FROM conversation WHERE id=$1::uuid`, cid).Scan(&row.Type, &row.Creator, &row.Access, &row.AccessRoles, &row.Name, &row.Team, &row.MessageTimer, &row.ReceiptMode, &row.Protocol, &row.GroupID, &row.Epoch, &row.EpochTime, &row.CipherSuite, &row.GroupType, &row.AddPermission, &row.CellsState, &row.Parent, &row.HistoryDepth)
	if errors.Is(err, pgx.ErrNoRows) {
		if err = tx.Commit(ctx); err != nil {
			return nil, postgresFailure(err, "conversation read commit failed")
		}
		return nil, nil
	}
	if err != nil || !row.valid() {
		return nil, postgresFailure(err, "invalid stored conversation metadata")
	}
	conv := &storedConversation{ID: id, Metadata: row.values()}
	if row.Team.Valid {
		team := postgresUUIDString(row.Team)
		conv.Team = &team
	}
	locals, err := tx.Query(ctx, `SELECT conv,"user",service,provider,otr_muted_status,otr_muted_ref,otr_archived,otr_archived_ref,hidden,hidden_ref,conversation_role FROM conversation_member WHERE conv=$1::uuid UNION ALL SELECT conv,"user",service,provider,otr_muted_status,otr_muted_ref,otr_archived,otr_archived_ref,hidden,hidden_ref,conversation_role FROM conversation_member WHERE conv IN (SELECT parent_conv FROM conversation WHERE id=$1::uuid)`, cid)
	if err != nil {
		return nil, postgresFailure(err, "conversation local member query failed")
	}
	var childLocals, parentLocals []localConversationMember
	for locals.Next() {
		var memberConv, user, service, provider pgtype.UUID
		var muted *int32
		var mutedRef, archivedRef, hiddenRef, role *string
		var archived, hidden *bool
		err = locals.Scan(&memberConv, &user, &service, &provider, &muted, &mutedRef, &archived, &archivedRef, &hidden, &hiddenRef, &role)
		if err != nil || !memberConv.Valid || !user.Valid || role != nil && !validConversationRole(*role) {
			locals.Close()
			return nil, postgresFailure(err, "invalid stored local conversation member")
		}
		uid := postgresUUIDString(user)
		m := localConversationMember{ID: uid, Role: "wire_admin"}
		if role != nil {
			m.Role = *role
		}
		if service.Valid && provider.Valid {
			serviceID, providerID := postgresUUIDString(service), postgresUUIDString(provider)
			m.Service, m.Provider = &serviceID, &providerID
		}
		if memberConv.Bytes == cid.Bytes {
			childLocals = append(childLocals, m)
		} else {
			parentLocals = append(parentLocals, m)
		}
	}
	err = locals.Err()
	locals.Close()
	if err != nil {
		return nil, postgresFailure(err, "conversation local member query failed")
	}
	// Do not rely on UNION ALL result order: the source explicitly prioritizes
	// child members and then deduplicates by user, retaining stable row order.
	seenLocal := make(map[string]bool)
	for _, m := range append(childLocals, parentLocals...) {
		if !seenLocal[m.ID] {
			conv.LocalMembers = append(conv.LocalMembers, m)
			seenLocal[m.ID] = true
		}
	}
	remotes, err := tx.Query(ctx, `SELECT conv,user_remote_domain,user_remote_id,conversation_role FROM local_conversation_remote_member WHERE conv=$1::uuid UNION ALL SELECT conv,user_remote_domain,user_remote_id,conversation_role FROM local_conversation_remote_member WHERE conv IN (SELECT parent_conv FROM conversation WHERE id=$1::uuid)`, cid)
	if err != nil {
		return nil, postgresFailure(err, "conversation remote member query failed")
	}
	var childRemotes, parentRemotes []remoteConversationMember
	for remotes.Next() {
		var memberConv, user pgtype.UUID
		var domain, role string
		err = remotes.Scan(&memberConv, &domain, &user, &role)
		if err != nil || !memberConv.Valid || !user.Valid || !validDomain(domain) || !validConversationRole(role) {
			remotes.Close()
			return nil, postgresFailure(err, "invalid stored remote conversation member")
		}
		uid := qualifiedUser{ID: postgresUUIDString(user), Domain: strings.ToLower(domain)}
		m := remoteConversationMember{User: uid, Role: role}
		if memberConv.Bytes == cid.Bytes {
			childRemotes = append(childRemotes, m)
		} else {
			parentRemotes = append(parentRemotes, m)
		}
	}
	err = remotes.Err()
	remotes.Close()
	if err != nil {
		return nil, postgresFailure(err, "conversation remote member query failed")
	}
	seenRemote := make(map[qualifiedUser]bool)
	for _, m := range append(childRemotes, parentRemotes...) {
		if !seenRemote[m.User] {
			conv.RemoteMembers = append(conv.RemoteMembers, m)
			seenRemote[m.User] = true
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, postgresFailure(err, "conversation read commit failed")
	}
	// Source toConv returns Nothing for MLS/mixed without group_id, after it
	// has loaded members. Empty group IDs are present values, not missing IDs.
	if row.Protocol != nil && *row.Protocol != 0 && row.GroupID == nil {
		return nil, nil
	}
	return conv, nil
}

func (s *postgresStore) upsertLocalMembersOnce(ctx context.Context, conv string, users []string, role string) error {
	cid, err := postgresUUID(conv)
	if err != nil || !validConversationRole(role) {
		return errors.New("invalid conversation member update")
	}
	ids := make([]pgtype.UUID, len(users))
	for i, user := range users {
		ids[i], err = postgresUUID(user)
		if err != nil {
			return err
		}
	}
	conn, err := s.acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadWrite})
	if err != nil {
		return postgresFailure(err, "conversation member transaction failed")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, id := range ids {
		if _, err = tx.Exec(ctx, `INSERT INTO conversation_member (conv,"user",conversation_role) VALUES ($1::uuid,$2::uuid,$3::text) ON CONFLICT (conv,"user") DO UPDATE SET conversation_role=$3::text`, cid, id, role); err != nil {
			return postgresFailure(err, "conversation member write failed")
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return postgresFailure(err, "conversation member commit failed")
	}
	return nil
}

var _ groupSyncStore = (*postgresStore)(nil)
