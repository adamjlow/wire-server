// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
)

type conversationMigrationMLSClient struct {
	GroupID        []byte
	Domain         string
	User           pgtype.UUID
	Client         string
	LeafIndex      int32
	RemovalPending bool
}

type migrationHistoryClient struct {
	ID             pgtype.UUID
	LeafIndex      int32
	RemovalPending bool
}

func decodeMigrationMLSClient(group []byte, row migrationRow) (conversationMigrationMLSClient, error) {
	invalid := errors.New("invalid stored migration MLS client")
	client := conversationMigrationMLSClient{GroupID: group}
	domain, err := userMigrationString(row["user_domain"])
	if err != nil || !validDomain(domain) || group == nil {
		return client, invalid
	}
	client.Domain = strings.ToLower(domain)
	user, ok := cqlUUID(row["user"])
	if !ok || !user.Valid {
		return client, invalid
	}
	client.User = user
	text, err := userMigrationString(row["client"])
	if err != nil || text == "" || !uriHex(text) {
		return client, invalid
	}
	text = strings.TrimLeft(text, "0")
	if text == "" {
		text = "0"
	}
	word, err := strconv.ParseUint(text, 16, 64)
	if err != nil {
		return client, invalid
	}
	client.Client = strconv.FormatUint(word, 16)
	index, err := userMigrationNullableInt32(row["leaf_node_index"])
	if err != nil || index == nil {
		return client, invalid
	}
	client.LeafIndex = *index
	pending, ok := row["removal_pending"].(bool)
	if !ok {
		return client, invalid
	}
	client.RemovalPending = pending
	return client, nil
}

// Wire.ConversationStore.MLS.Types builds ClientMap from nonpending regular
// rows, then IndexMap by foldr of regular rows followed by foldr of history.
// History takes precedence at a colliding index, and migration never inserts
// history identities into the regular client table. Pending is recomputed from
// ClientMap membership, not copied from each surviving index row.
func migrationMLSClients(regular []conversationMigrationMLSClient, history []migrationHistoryClient) []conversationMigrationMLSClient {
	identity := func(client conversationMigrationMLSClient) string {
		return client.Domain + "/" + postgresUUIDString(client.User) + "/" + client.Client
	}
	active := make(map[string]bool)
	indices := make(map[int32]conversationMigrationMLSClient)
	for _, client := range regular {
		if !client.RemovalPending {
			active[identity(client)] = true
		}
		if _, present := indices[client.LeafIndex]; !present {
			indices[client.LeafIndex] = client // Native foldr: earliest row wins.
		}
	}
	for _, client := range history {
		delete(indices, client.LeafIndex)
	}
	keys := make([]int32, 0, len(indices))
	for index := range indices {
		keys = append(keys, index)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	result := make([]conversationMigrationMLSClient, 0, len(keys))
	for _, index := range keys {
		client := indices[index]
		client.RemovalPending = !active[identity(client)]
		result = append(result, client)
	}
	return result
}

func (s *postgresStore) migrationHistoryClients(ctx context.Context, group []byte) (history []migrationHistoryClient, err error) {
	err = retryPostgres(ctx, s.Pool.Reset, func() error {
		conn, err := s.acquire(ctx)
		if err != nil {
			return err
		}
		defer conn.Release()
		rows, err := conn.Query(ctx, `SELECT id,leaf_node_index,removal_pending FROM mls_history_client WHERE group_id=$1::bytea`, group)
		if err != nil {
			return postgresFailure(err, "migration MLS history lookup failed")
		}
		defer rows.Close()
		history = nil
		for rows.Next() {
			var client migrationHistoryClient
			if err := rows.Scan(&client.ID, &client.LeafIndex, &client.RemovalPending); err != nil || !client.ID.Valid {
				return postgresFailure(err, "invalid migration MLS history row")
			}
			history = append(history, client)
		}
		if err := rows.Err(); err != nil {
			return postgresFailure(err, "migration MLS history rows failed")
		}
		return nil
	})
	return
}

func (s cassandraConversationStore) migrationMLSClients(ctx context.Context, group []byte, postgres *postgresStore) ([]conversationMigrationMLSClient, error) {
	iter := s.query(ctx, `SELECT user_domain,user,client,leaf_node_index,removal_pending FROM mls_group_member_client WHERE group_id=?`, group).RetryPolicy(cassandraRetry(5)).Iter()
	row := nullableCassandraRow("user_domain", "user", "client", "leaf_node_index", "removal_pending")
	var regular []conversationMigrationMLSClient
	for iter.MapScan(row) {
		unwrapCassandraRow(row)
		client, err := decodeMigrationMLSClient(group, migrationRow(row))
		if err != nil {
			_ = iter.Close()
			return nil, err
		}
		regular = append(regular, client)
		row = nullableCassandraRow("user_domain", "user", "client", "leaf_node_index", "removal_pending")
	}
	if iter.Close() != nil {
		return nil, errors.New("Cassandra migration MLS client rows failed")
	}
	history, err := postgres.migrationHistoryClients(ctx, group)
	if err != nil {
		return nil, err
	}
	return migrationMLSClients(regular, history), nil
}
