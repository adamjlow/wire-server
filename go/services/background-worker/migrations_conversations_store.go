// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type conversationMigrationMember struct {
	User, Service, Provider pgtype.UUID
	Role                    string
	Status                  remoteConversationStatus
}

type conversationMigrationRemoteMember struct {
	User   pgtype.UUID
	Domain string
	Role   string
}

type conversationMigrationSub struct {
	ID          string
	GroupID     []byte
	CipherSuite *int32
	Epoch       int64
	EpochTime   time.Time
	GroupState  []byte
}

type conversationMigrationRecord struct {
	ID         pgtype.UUID
	Metadata   conversationMetadataRow
	GroupState []byte
	Members    []conversationMigrationMember
	Remotes    []conversationMigrationRemoteMember
	Subs       []conversationMigrationSub
	MLSClients []conversationMigrationMLSClient
}

func writeConversationMigration(ctx context.Context, tx userMigrationSQL, row conversationMigrationRecord) error {
	if !row.ID.Valid || !row.Metadata.valid() {
		return errors.New("invalid conversation migration record")
	}
	exec := func(sql string, args ...any) error {
		if _, err := tx.Exec(ctx, sql, args...); err != nil {
			return postgresFailure(err, "conversation migration transaction write failed")
		}
		return nil
	}
	m := row.Metadata
	protocol, cells := int32(0), int32(0)
	if m.Protocol != nil {
		protocol = *m.Protocol
	}
	if m.CellsState != nil {
		cells = *m.CellsState
	}
	if err := exec(`INSERT INTO conversation (id,type,creator,access,access_roles_v2,name,team,message_timer,receipt_mode,protocol,group_id,epoch,epoch_timestamp,cipher_suite,public_group_state,group_conv_type,channel_add_permission,cells_state,parent_conv,history_depth) VALUES ($1::uuid,$2::integer,$3::uuid,$4::integer[],$5::integer[],$6::text,$7::uuid,$8::bigint,$9::integer,$10::integer,$11::bytea,$12::bigint,$13::timestamptz,$14::integer,$15::bytea,$16::integer,$17::integer,$18::integer,$19::uuid,$20::bigint) ON CONFLICT (id) DO NOTHING`, row.ID, m.Type, m.Creator, m.Access, m.AccessRoles, m.Name, m.Team, m.MessageTimer, m.ReceiptMode, protocol, m.GroupID, m.Epoch, m.EpochTime, m.CipherSuite, row.GroupState, m.GroupType, m.AddPermission, cells, m.Parent, m.HistoryDepth); err != nil {
		return err
	}
	n := len(row.Members)
	convs, users, services, providers := make([]pgtype.UUID, n), make([]pgtype.UUID, n), make([]pgtype.UUID, n), make([]pgtype.UUID, n)
	muted := make([]*int32, n)
	mutedRef, archivedRef, hiddenRef := make([]*string, n), make([]*string, n), make([]*string, n)
	archived, hidden, roles := make([]bool, n), make([]bool, n), make([]string, n)
	for i, member := range row.Members {
		convs[i], users[i], services[i], providers[i] = row.ID, member.User, member.Service, member.Provider
		muted[i], mutedRef[i], archived[i], archivedRef[i], hidden[i], hiddenRef[i], roles[i] = member.Status.Muted, member.Status.MutedRef, member.Status.Archived, member.Status.ArchivedRef, member.Status.Hidden, member.Status.HiddenRef, member.Role
	}
	if err := exec(`INSERT INTO conversation_member (conv,"user",service,provider,otr_muted_status,otr_muted_ref,otr_archived,otr_archived_ref,hidden,hidden_ref,conversation_role) SELECT * FROM UNNEST($1::uuid[],$2::uuid[],$3::uuid[],$4::uuid[],$5::integer[],$6::text[],$7::boolean[],$8::text[],$9::boolean[],$10::text[],$11::text[]) ON CONFLICT (conv,"user") DO NOTHING`, convs, users, services, providers, muted, mutedRef, archived, archivedRef, hidden, hiddenRef, roles); err != nil {
		return err
	}
	n = len(row.Remotes)
	convs, users, domains := make([]pgtype.UUID, n), make([]pgtype.UUID, n), make([]string, n)
	roles = make([]string, n)
	for i, member := range row.Remotes {
		convs[i], users[i], domains[i], roles[i] = row.ID, member.User, member.Domain, member.Role
	}
	if err := exec(`INSERT INTO local_conversation_remote_member (conv,user_remote_domain,user_remote_id,conversation_role) SELECT * FROM UNNEST($1::uuid[],$2::text[],$3::uuid[],$4::text[])`, convs, domains, users, roles); err != nil {
		return err
	}
	n = len(row.Subs)
	convs, subIDs := make([]pgtype.UUID, n), make([]string, n)
	ciphers, epochs, times := make([]*int32, n), make([]int64, n), make([]time.Time, n)
	groups, states := make([][]byte, n), make([][]byte, n)
	for i, sub := range row.Subs {
		convs[i], subIDs[i], ciphers[i], epochs[i], times[i], groups[i], states[i] = row.ID, sub.ID, sub.CipherSuite, sub.Epoch, sub.EpochTime, sub.GroupID, sub.GroupState
	}
	if err := exec(`INSERT INTO subconversation (conv_id,subconv_id,cipher_suite,epoch,epoch_timestamp,group_id,public_group_state) SELECT * FROM UNNEST($1::uuid[],$2::text[],$3::integer[],$4::bigint[],$5::timestamptz[],$6::bytea[],$7::bytea[])`, convs, subIDs, ciphers, epochs, times, groups, states); err != nil {
		return err
	}
	n = len(row.MLSClients)
	groups, domains, users, clients, indices, pending := make([][]byte, n), make([]string, n), make([]pgtype.UUID, n), make([]string, n), make([]int32, n), make([]bool, n)
	for i, client := range row.MLSClients {
		groups[i], domains[i], users[i], clients[i], indices[i], pending[i] = client.GroupID, client.Domain, client.User, client.Client, client.LeafIndex, client.RemovalPending
	}
	if err := exec(`INSERT INTO mls_group_member_client (group_id,user_domain,"user",client,leaf_node_index,removal_pending) SELECT * FROM UNNEST($1::bytea[],$2::text[],$3::uuid[],$4::text[],$5::integer[],$6::bool[])`, groups, domains, users, clients, indices, pending); err != nil {
		return err
	}
	// Preserve the exact native conflict policy per table. Only conversation,
	// local members and the pending ledger have ON CONFLICT DO NOTHING.
	return exec(`INSERT INTO conversation_migration_pending_deletes (typ,id) VALUES ('conv',$1::uuid) ON CONFLICT DO NOTHING`, row.ID)
}

func (s *postgresStore) saveConversationMigration(ctx context.Context, row conversationMigrationRecord) error {
	return retryPostgres(ctx, s.Pool.Reset, func() error {
		conn, err := s.acquire(ctx)
		if err != nil {
			return err
		}
		defer conn.Release()
		_, err = retryUserMigrationTransaction(ctx, func() (bool, error) {
			tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadWrite})
			if err != nil {
				return false, postgresFailure(err, "conversation migration transaction start failed")
			}
			defer func() { _ = tx.Rollback(ctx) }()
			if err := writeConversationMigration(ctx, tx, row); err != nil {
				return false, err
			}
			if err := tx.Commit(ctx); err != nil {
				return false, postgresFailure(err, "conversation migration transaction commit failed")
			}
			return false, nil
		})
		return err
	})
}
