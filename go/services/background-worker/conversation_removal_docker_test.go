//go:build schemacheck

// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// Cassandra proposals are deliberately stubbed in this PostgreSQL qualifier.
// It proves SQL effects and orchestration, not cross-store/service parity.
type removalPGFixture struct {
	scheduledRemovalStore
	proposals [][]byte
}

func (f *removalPGFixture) DeleteProposals(_ context.Context, g []byte) error {
	f.proposals = append(f.proposals, append([]byte(nil), g...))
	return nil
}

func TestConversationRemovalDockerStorage(t *testing.T) {
	if os.Getenv("WIRE_SCHEMA_CHECK_SYNTHETIC") != "1" {
		t.Fatal("requires owned synthetic Docker")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	store, err := openPostgres(ctx, PostgresPoolConfig{Size: 4, Acquisition: Duration(5 * time.Second), Idleness: Duration(time.Minute)}, GalleyConfig{Postgres: map[string]string{"host": "postgres", "port": "5432", "user": "postgres", "password": "synthetic-schema-only", "dbname": "backendA", "sslmode": "disable"}})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Pool.Close()
	conn, err := store.acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := conn.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"20250908142515-create-conversation-tables.sql", "20260507162106-history_client.sql", "20260115150600-conversation-codes.sql", "20260619120000-conversation-codes-target.sql"} {
		b, err := os.ReadFile(filepath.Join("/reference/migrations", name))
		if err != nil {
			t.Fatal(err)
		}
		exec(string(b))
	}
	id := "00000000-0000-0000-0000-000000000001"
	untouched := "00000000-0000-0000-0000-000000000002"
	exec(`INSERT INTO conversation(id,type,protocol) VALUES($1::uuid,0,1),($2::uuid,0,1)`, id, untouched)
	exec(`INSERT INTO conversation_member(conv,"user") VALUES($1::uuid,$2::uuid)`, id, untouched)
	exec(`INSERT INTO local_conversation_remote_member(conv,user_remote_domain,user_remote_id) VALUES($1::uuid,'synthetic.example',$2::uuid)`, id, untouched)
	// Adminless conversation adapters use these same native tables.
	exec(`UPDATE conversation SET team=$1::uuid WHERE id=$1::uuid`, id)
	admin := adminlessConversationStore{Data: scheduledRemovalStore{Postgres: store, Location: storagePostgres}, Domain: "wire.com"}
	ids, err := admin.TeamConversations(ctx, id)
	if err != nil || !reflect.DeepEqual(ids, []string{id}) {
		t.Fatalf("team scan %v: %v", ids, err)
	}
	if err := admin.Promote(ctx, &storedConversation{ID: id}, qualifiedUser{ID: untouched, Domain: "wire.com"}); err != nil {
		t.Fatal(err)
	}
	var role string
	if err := conn.QueryRow(ctx, `SELECT conversation_role FROM conversation_member WHERE conv=$1::uuid AND "user"=$2::uuid`, id, untouched).Scan(&role); err != nil || role != "wire_admin" {
		t.Fatal("promotion not stored")
	}
	if err := admin.Promote(ctx, &storedConversation{ID: id}, qualifiedUser{ID: untouched, Domain: "remote.example"}); err == nil {
		t.Fatal("remote promotion accepted on local adapter")
	}
	if err := admin.Promote(ctx, &storedConversation{ID: id}, qualifiedUser{ID: id, Domain: "wire.com"}); err != nil {
		t.Fatal("missing-member SQL update changed source no-op")
	}
	for _, g := range []string{"main", "a", "z", "unrelated"} {
		exec(`INSERT INTO mls_group_member_client(group_id,user_domain,"user",client,leaf_node_index,removal_pending) VALUES($1,'synthetic.example',$2::uuid,'synthetic',1,false)`, []byte(g), id)
		exec(`INSERT INTO mls_history_client(group_id,id,leaf_node_index,removal_pending) VALUES($1,$2::uuid,1,false)`, []byte(g), id)
	}
	for _, sub := range []string{"z", "a"} {
		exec(`INSERT INTO subconversation(conv_id,subconv_id,cipher_suite,epoch,epoch_timestamp,group_id) VALUES($1::uuid,$2,1,0,NOW(),$3)`, id, sub, []byte(sub))
	}
	hash := sha256.Sum256([]byte(id))
	key := base64.RawURLEncoding.EncodeToString(hash[:15])
	exec(`INSERT INTO conversation_codes(key,conversation,value,expires_at,target) VALUES($1,$2::uuid,'synthetic',NOW()+INTERVAL '1 hour','conv'),('unrelated',$3::uuid,'synthetic',NOW()+INTERVAL '1 hour','conv')`, key, id, untouched)
	state := &removalPGFixture{scheduledRemovalStore: scheduledRemovalStore{Postgres: store, Location: storagePostgres, Codes: storagePostgres}}
	// Full native-row decode must fail on an inactive epoch's bad cipher too.
	exec(`UPDATE subconversation SET cipher_suite=3 WHERE subconv_id='z'`)
	if _, err := state.SubConversations(ctx, id); err == nil {
		t.Fatal("invalid inactive cipher accepted")
	}
	exec(`UPDATE subconversation SET cipher_suite=1 WHERE subconv_id='z'`)
	subs, err := state.SubConversations(ctx, id)
	if err != nil || !reflect.DeepEqual(subs, []removalSubConversation{{ID: "a", Group: []byte("a")}, {ID: "z", Group: []byte("z")}}) {
		t.Fatalf("subconversations %v: %v", subs, err)
	}
	read := &removalFixture{conv: &storedConversation{ID: id, Metadata: map[string]any{"protocol": int32(1), "group_id": []byte("main")}}}
	if err := (scheduledConversationRemoval{Read: read, State: state}).DeleteLocalConversationUnchecked(ctx, id); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(state.proposals, [][]byte{[]byte("a"), []byte("z"), []byte("main")}) {
		t.Fatal("proposal callback order changed")
	}
	for _, sql := range []string{`SELECT count(*) FROM conversation`, `SELECT count(*) FROM conversation_codes`, `SELECT count(*) FROM mls_group_member_client`, `SELECT count(*) FROM mls_history_client`} {
		var n int
		if err := conn.QueryRow(ctx, sql).Scan(&n); err != nil || n != 1 {
			t.Fatalf("unrelated rows lost: %s count=%d error=%v", sql, n, err)
		}
	}
	for _, sql := range []string{`SELECT count(*) FROM subconversation`, `SELECT count(*) FROM conversation_member`, `SELECT count(*) FROM local_conversation_remote_member`} {
		var n int
		if err := conn.QueryRow(ctx, sql).Scan(&n); err != nil || n != 0 {
			t.Fatalf("child rows retained: %s count=%d error=%v", sql, n, err)
		}
	}
	// Storage deletes are idempotent; the subsystem still rereads before deletion.
	if err := state.DeleteConversationState(ctx, id, nil); err != nil {
		t.Fatal(err)
	}
	t.Log("readonly native DDL: adminless team scan/local promotion and guards, typed sub-row rejection, sorted MLS/history deletion, code isolation, conversation FK cascade and idempotent state deletion pass; read and Cassandra proposal callbacks stubbed, no service/cross-store claim")
}
