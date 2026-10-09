// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"reflect"
	"strings"
	"testing"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func TestMigrationMLSClientDecodingPreservesNativeEnumsAndCanonicalIdentity(t *testing.T) {
	id, _ := postgresUUID(jobUUID)
	row := migrationRow{"user_domain": "Remote.Example.COM", "user": gocql.UUID(id.Bytes), "client": "00000000000000000AF", "leaf_node_index": int32(-1), "removal_pending": false}
	client, err := decodeMigrationMLSClient([]byte{}, row)
	if err != nil || client.Domain != "remote.example.com" || client.Client != "af" || client.LeafIndex != -1 || client.RemovalPending || client.GroupID == nil {
		t.Fatal("native domain/client canonicalization or signed index changed")
	}
	for column, value := range map[string]any{"user_domain": "private invalid domain", "user": nil, "client": "10000000000000000", "leaf_node_index": nil, "removal_pending": nil} {
		bad := make(migrationRow)
		for key, val := range row {
			bad[key] = val
		}
		bad[column] = value
		if _, err := decodeMigrationMLSClient([]byte{1}, bad); err == nil || strings.Contains(err.Error(), "private") {
			t.Fatal("invalid typed MLS row accepted or values exposed")
		}
	}
}

func TestMigrationMLSIndexMapUsesHistoryPrecedenceAndActiveIdentityMembership(t *testing.T) {
	id, _ := postgresUUID(jobUUID)
	regular := []conversationMigrationMLSClient{
		{Domain: "remote.example.com", User: id, Client: "a", LeafIndex: 3, RemovalPending: true},
		{Domain: "remote.example.com", User: id, Client: "b", LeafIndex: 3, RemovalPending: false},
		{Domain: "remote.example.com", User: id, Client: "a", LeafIndex: 4, RemovalPending: false},
		{Domain: "remote.example.com", User: id, Client: "c", LeafIndex: -1, RemovalPending: true},
		{Domain: "remote.example.com", User: id, Client: "d", LeafIndex: 9, RemovalPending: false},
	}
	history := []migrationHistoryClient{{ID: id, LeafIndex: 4}, {ID: id, LeafIndex: 9, RemovalPending: true}}
	actual := migrationMLSClients(regular, history)
	want := []conversationMigrationMLSClient{regular[3], regular[0]}
	want[1].RemovalPending = false // Same identity active at history-shadowed index4.
	if !reflect.DeepEqual(actual, want) {
		t.Fatal("native earliest-row/index/history/client-map semantics changed")
	}
	if len(regular) != 5 || !regular[0].RemovalPending || len(history) != 2 {
		t.Fatal("source input slices mutated while constructing copy")
	}
}
