// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type cassandraSubConversation struct {
	ID                    string
	GroupID               []byte
	CipherSuite           int32
	Epoch, EpochWriteTime int64
}

func decodeCassandraSubConversation(row map[string]any) (cassandraSubConversation, error) {
	var sub cassandraSubConversation
	var ok bool
	sub.ID, ok = row["subconv_id"].(string)
	if !ok {
		return sub, errors.New("invalid stored subconversation")
	}
	sub.GroupID, ok = row["group_id"].([]byte)
	if !ok || sub.GroupID == nil {
		return sub, errors.New("invalid stored subconversation group")
	}
	cs, valid := cqlInteger(row["cipher_suite"])
	if !valid {
		return sub, errors.New("invalid stored subconversation cipher")
	}
	switch uint16(cs) {
	case 1, 2, 5, 7:
	default:
		return sub, errors.New("invalid stored subconversation cipher")
	}
	sub.CipherSuite = int32(cs)
	sub.Epoch, ok = cqlInteger(row["epoch"])
	if !ok {
		return sub, errors.New("invalid stored subconversation epoch")
	}
	sub.EpochWriteTime, ok = cqlInteger(row["writetime(epoch)"])
	if !ok {
		return sub, errors.New("invalid stored subconversation epoch timestamp")
	}
	return sub, nil
}

func (s cassandraConversationStore) subConversations(ctx context.Context, cid gocql.UUID) ([]cassandraSubConversation, error) {
	iter := s.query(ctx, `SELECT subconv_id,cipher_suite,epoch,WRITETIME(epoch),group_id FROM subconversation WHERE conv_id=?`, cid).Iter()
	var subs []cassandraSubConversation
	row := nullableCassandraRow("subconv_id", "cipher_suite", "epoch", "writetime(epoch)", "group_id")
	for iter.MapScan(row) {
		unwrapCassandraRow(row)
		sub, err := decodeCassandraSubConversation(row)
		if err != nil {
			_ = iter.Close()
			return nil, err
		}
		subs = append(subs, sub)
		row = nullableCassandraRow("subconv_id", "cipher_suite", "epoch", "writetime(epoch)", "group_id")
	}
	if err := iter.Close(); err != nil {
		return nil, errors.New("Cassandra subconversation query failed")
	}
	return subs, nil
}

func (s cassandraConversationStore) readGroupState(ctx context.Context, cid gocql.UUID, sub *string) error {
	query := s.query(ctx, `SELECT public_group_state FROM conversation WHERE conv=?`, cid)
	if sub != nil {
		query = s.query(ctx, `SELECT public_group_state FROM subconversation WHERE conv_id=? AND subconv_id=?`, cid, *sub).RetryPolicy(cassandraRetry(5))
	}
	var state []byte
	if err := query.Scan(&state); err != nil && !errors.Is(err, gocql.ErrNotFound) {
		return errors.New("Cassandra group state query failed")
	}
	return nil
}

func validStoredMLSClient(row map[string]any) bool {
	domain, ok := row["user_domain"].(string)
	if !ok || !validDomain(domain) {
		return false
	}
	user, ok := cqlUUID(row["user"])
	if !ok || !user.Valid {
		return false
	}
	client, ok := row["client"].(string)
	if !ok || client == "" {
		return false
	}
	for _, c := range client {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	text := strings.TrimLeft(client, "0")
	if text == "" {
		text = "0"
	}
	if _, err := strconv.ParseUint(text, 16, 64); err != nil {
		return false
	}
	_, indexOK := cqlInteger(row["leaf_node_index"])
	_, pendingOK := row["removal_pending"].(bool)
	return indexOK && pendingOK
}

func (s cassandraConversationStore) readMLSClients(ctx context.Context, group []byte) error {
	iter := s.query(ctx, `SELECT user_domain,user,client,leaf_node_index,removal_pending FROM mls_group_member_client WHERE group_id=?`, group).RetryPolicy(cassandraRetry(5)).Iter()
	row := nullableCassandraRow("user_domain", "user", "client", "leaf_node_index", "removal_pending")
	for iter.MapScan(row) {
		unwrapCassandraRow(row)
		if !validStoredMLSClient(row) {
			_ = iter.Close()
			return errors.New("invalid stored MLS client")
		}
		row = nullableCassandraRow("user_domain", "user", "client", "leaf_node_index", "removal_pending")
	}
	if iter.Close() != nil {
		return errors.New("Cassandra MLS client query failed")
	}
	return nil
}

func (s cassandraConversationStore) CleanupMigratedConversation(ctx context.Context, id string) error {
	conv, err := s.Conversation(ctx, id)
	if err != nil || conv == nil {
		return err
	}
	cid, _ := parseUserID(id)
	subs, err := s.subConversations(ctx, cid)
	if err != nil {
		return err
	}
	if err := s.readGroupState(ctx, cid, nil); err != nil {
		return err
	}
	group, _ := conv.Metadata["group_id"].([]byte)
	protocol, _ := conv.Metadata["protocol"].(int32)
	if protocol != 0 {
		if err := s.readMLSClients(ctx, group); err != nil {
			return err
		}
	}
	// Native getAllConvData completes all reads/validation before deleteConv.
	// Failure here must leave both Cassandra and the PG pending ledger intact.
	for _, sub := range subs {
		if err := s.readMLSClients(ctx, sub.GroupID); err != nil {
			return err
		}
		if err := s.readGroupState(ctx, cid, &sub.ID); err != nil {
			return err
		}
	}
	for _, sub := range subs {
		if err := s.query(ctx, `DELETE FROM mls_group_member_client WHERE group_id=?`, sub.GroupID).RetryPolicy(cassandraRetry(5)).Exec(); err != nil {
			return errors.New("Cassandra subconversation clients deletion failed")
		}
		if err := s.query(ctx, `DELETE FROM subconversation WHERE conv_id=? AND subconv_id=?`, cid, sub.ID).RetryPolicy(cassandraRetry(5)).Exec(); err != nil {
			return errors.New("Cassandra subconversation deletion failed")
		}
	}
	if protocol != 0 {
		if err := s.query(ctx, `DELETE FROM mls_group_member_client WHERE group_id=?`, group).RetryPolicy(cassandraRetry(5)).Exec(); err != nil {
			return errors.New("Cassandra conversation clients deletion failed")
		}
	}
	if conv.Team != nil {
		team, err := parseUserID(*conv.Team)
		if err != nil {
			return err
		}
		batch := s.Session.NewBatch(gocql.LoggedBatch).WithContext(ctx)
		batch.Cons = gocql.LocalQuorum
		batch.RetryPolicy(cassandraRetry(5))
		batch.Query(`UPDATE conversation SET deleted=true WHERE conv=?`, cid)
		batch.Query(`DELETE FROM team_conv WHERE team=? AND conv=?`, team, cid)
		if err := s.Session.ExecuteBatch(batch); err != nil {
			return errors.New("Cassandra team conversation deletion failed")
		}
	}
	return s.DeleteConversation(ctx, id)
}
