// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestConversationMigrationMetadataMatchesStoredDefaultsAndProtocol(t *testing.T) {
	for typ := int32(0); typ <= 3; typ++ {
		row := normalizeConversationMigrationMetadata(conversationMetadataRow{Type: typ})
		access, roles := []int32{1}, []int32{}
		if typ == 0 {
			access, roles = []int32{2}, []int32{1, 2, 4}
		}
		if !reflect.DeepEqual(row.Access, access) || !reflect.DeepEqual(row.AccessRoles, roles) {
			t.Fatal("stored conversation access/role default changed")
		}
	}
	row := normalizeConversationMigrationMetadata(conversationMetadataRow{Access: []int32{3, 2}, AccessRoles: []int32{4, 1, 4}})
	if !reflect.DeepEqual(row.Access, []int32{3, 2}) || !reflect.DeepEqual(row.AccessRoles, []int32{1, 4}) {
		t.Fatal("native access list vs role set distinction lost")
	}
	row = normalizeConversationMigrationMetadata(conversationMetadataRow{AccessRoles: []int32{}})
	if len(row.AccessRoles) != 0 || row.AccessRoles == nil {
		t.Fatal("present private role set defaulted to activated")
	}
	protocol, cipher, epoch := int32(1), int32(65537), int64(0)
	stamp := time.UnixMicro(1700000000000001)
	complete := conversationMetadataRow{Protocol: &protocol, GroupID: []byte{}, CipherSuite: &cipher, Epoch: &epoch, EpochTime: &stamp}
	row = normalizeConversationMigrationMetadata(complete)
	if row.GroupID == nil || row.Epoch == nil || *row.Epoch != 0 || *row.CipherSuite != 1 || !row.EpochTime.Equal(stamp) {
		t.Fatal("main MLS epoch0 or native Word16 cipher conversion changed")
	}
	complete.EpochTime = nil
	row = normalizeConversationMigrationMetadata(complete)
	if row.GroupID == nil || row.Epoch != nil || row.CipherSuite != nil || row.EpochTime != nil {
		t.Fatal("partially active MLS fields were copied")
	}
	complete.Protocol = nil
	row = normalizeConversationMigrationMetadata(complete)
	if row.GroupID != nil || row.Epoch != nil || row.CipherSuite != nil || row.EpochTime != nil {
		t.Fatal("Proteus retained unrelated raw MLS columns")
	}
}

func TestConversationMigrationMemberTypesBeforeInactiveFilterAndStoredRoles(t *testing.T) {
	id, _ := postgresUUID(jobUUID)
	row := migrationRow{"user": gocql.UUID(id.Bytes), "service": gocql.UUID(id.Bytes), "status": int32(0), "conversation_role": "Legacy role with spaces", "otr_muted_status": int32(-9), "otr_muted_ref": "", "otr_archived": true}
	member, active, err := decodeConversationMigrationMember(row)
	if err != nil || !active || member.Service.Valid || member.Provider.Valid || member.Role != row["conversation_role"] || *member.Status.Muted != -9 || *member.Status.MutedRef != "" || !member.Status.Archived || member.Status.Hidden {
		t.Fatal("native service pairing/raw role/nullable status changed")
	}
	for _, column := range []string{"user", "service", "status", "otr_muted_ref", "hidden", "conversation_role"} {
		invalid := migrationRow{"user": gocql.UUID(id.Bytes), "status": int32(1)}
		invalid[column] = []byte("synthetic-private")
		if _, _, err := decodeConversationMigrationMember(invalid); err == nil || strings.Contains(err.Error(), "synthetic-private") {
			t.Fatal("malformed inactive member accepted or error leaked stored data")
		}
	}
	delete(row, "conversation_role")
	delete(row, "status")
	member, active, err = decodeConversationMigrationMember(row)
	if err != nil || active || member.Role != "wire_admin" {
		t.Fatal("missing status became active or missing role lost default")
	}
	remote, err := decodeConversationMigrationRemote(migrationRow{"user_remote_domain": "Remote.Example.COM", "user_remote_id": gocql.UUID(id.Bytes), "conversation_role": ""})
	if err != nil || remote.Domain != "remote.example.com" || remote.Role != "" {
		t.Fatal("stored remote role/domain normalization changed")
	}
}

func TestConversationMigrationInheritedMembersPreferChildAndDecodeDuplicates(t *testing.T) {
	id, _ := postgresUUID(jobUUID)
	parent := gocql.UUID(id.Bytes)
	parent[15]++
	malformedDuplicate := false
	query := func(_ context.Context, cql string, _ []string, retries int, args ...any) ([]migrationRow, error) {
		if retries != 1 {
			t.Fatal("member/parent source x1 retry changed")
		}
		if strings.HasPrefix(cql, "SELECT parent_conv") {
			return []migrationRow{{"parent_conv": parent}}, nil
		}
		if args[0] == parent {
			row := migrationRow{"user": gocql.UUID(id.Bytes), "status": int32(0), "conversation_role": "parent"}
			if malformedDuplicate {
				row["hidden"] = 1
			}
			return []migrationRow{row}, nil
		}
		return []migrationRow{{"user": gocql.UUID(id.Bytes), "status": int32(0), "conversation_role": "child", "hidden": true}}, nil
	}
	rows, err := readConversationMigrationMembers(t.Context(), gocql.UUID(id.Bytes), query)
	if err != nil || len(rows) != 1 || rows[0].Role != "child" || !rows[0].Status.Hidden {
		t.Fatal("inherited member replaced child membership/status")
	}
	malformedDuplicate = true
	if _, err := readConversationMigrationMembers(t.Context(), gocql.UUID(id.Bytes), query); err == nil {
		t.Fatal("deduplication hid malformed typed parent member")
	}
}

func TestConversationMigrationReaderMLSStateAndInactiveSubs(t *testing.T) {
	id, _ := postgresUUID(jobUUID)
	main := migrationRow{"type": int32(0), "protocol": int32(1), "group_id": []byte("main"), "epoch": int64(0), "writetime(epoch)": int64(1700000000000001), "cipher_suite": int32(65537)}
	mainState := []byte(nil)
	var mlsGroups []string
	query := func(_ context.Context, cql string, _ []string, retries int, _ ...any) ([]migrationRow, error) {
		if cql == conversationMigrationSelect {
			return []migrationRow{main}, nil
		}
		if cql == conversationMigrationSubSelect {
			return []migrationRow{
				{"subconv_id": "z", "group_id": []byte("z"), "cipher_suite": int32(65538), "epoch": int64(0), "writetime(epoch)": int64(99)},
				{"subconv_id": "a", "group_id": []byte("a"), "cipher_suite": int32(65541), "epoch": int64(-1), "writetime(epoch)": int64(1000001)},
			}, nil
		}
		if cql == `SELECT public_group_state FROM conversation WHERE conv=?` {
			if retries != 1 {
				t.Fatal("main group state retry changed")
			}
			return []migrationRow{{"public_group_state": mainState}}, nil
		}
		if strings.HasPrefix(cql, "SELECT public_group_state FROM subconversation") {
			if retries != 5 {
				t.Fatal("sub group state retry changed")
			}
			return []migrationRow{{"public_group_state": []byte{}}}, nil
		}
		return nil, nil
	}
	mls := func(_ context.Context, group []byte) ([]conversationMigrationMLSClient, error) {
		mlsGroups = append(mlsGroups, string(group))
		return []conversationMigrationMLSClient{{GroupID: group, User: id, Domain: "example.com", Client: "a"}}, nil
	}
	read := func() (*conversationMigrationRecord, error) {
		return readConversationMigration(t.Context(), id, query, mls, func(context.Context) error { t.Fatal("unmarked record deleted"); return nil })
	}
	row, err := read()
	if err != nil || row == nil || row.GroupState != nil || len(row.MLSClients) != 2 || !reflect.DeepEqual(mlsGroups, []string{"main", "a", "z"}) || len(row.Subs) != 2 {
		t.Fatal("absent main state skipped required MLS lookup or ordered sub traversal changed")
	}
	if row.Metadata.Epoch == nil || *row.Metadata.Epoch != 0 || *row.Metadata.CipherSuite != 1 || row.Subs[0].Epoch != -1 || *row.Subs[0].CipherSuite != 5 || !row.Subs[0].EpochTime.Equal(time.UnixMicro(1000001)) || row.Subs[1].CipherSuite != nil || row.Subs[1].Epoch != 0 || !row.Subs[1].EpochTime.Equal(time.Unix(0, 0)) || row.Subs[1].GroupState == nil {
		t.Fatal("main epoch0/sub epoch0/native casts/nullable empty state distinction lost")
	}
	mainState = []byte{}
	mlsGroups = nil
	row, err = read()
	if err != nil || row.GroupState == nil || len(row.MLSClients) != 3 {
		t.Fatal("present empty main state treated as absent")
	}
	main["group_id"] = nil
	mlsGroups = nil
	row, err = read()
	if err != nil || row != nil || len(mlsGroups) != 0 {
		t.Fatal("missing main MLS group did not follow toConv Nothing")
	}
	main["access_roles_v2"], main["access_role"] = []int32{1}, int32(9)
	if _, err := read(); err == nil {
		t.Fatal("present v2 access hid invalid typed legacy enum")
	}
	for _, cipher := range []any{nil, int32(3), int64(1<<32 + 1)} {
		if _, err := decodeCassandraSubConversation(map[string]any{"subconv_id": "a", "group_id": []byte{}, "cipher_suite": cipher, "epoch": int64(0), "writetime(epoch)": int64(0)}); err == nil {
			t.Fatal("inactive sub bypassed required cipher decoding")
		}
	}
}

func TestConversationMigrationSQLConflictPoliciesAndNullableColumns(t *testing.T) {
	id, _ := postgresUUID(jobUUID)
	muted, cipher := int32(0), int32(1)
	row := conversationMigrationRecord{ID: id, Metadata: normalizeConversationMigrationMetadata(conversationMetadataRow{}),
		Members:    []conversationMigrationMember{{User: id, Role: "raw role", Status: remoteConversationStatus{Muted: &muted, Hidden: true}}},
		Remotes:    []conversationMigrationRemoteMember{{User: id, Domain: "example.com", Role: "raw role"}},
		Subs:       []conversationMigrationSub{{ID: "a", GroupID: []byte{}, CipherSuite: &cipher, EpochTime: time.Unix(0, 0).UTC()}},
		MLSClients: []conversationMigrationMLSClient{{GroupID: []byte{}, User: id, Domain: "example.com", Client: "a", LeafIndex: -1, RemovalPending: true}}}
	tx := &observedUserSQL{}
	if err := writeConversationMigration(t.Context(), tx, row); err != nil {
		t.Fatal(err)
	}
	if len(tx.statements) != 6 {
		t.Fatal("native transaction statements changed")
	}
	for i, statement := range tx.statements {
		if strings.Contains(statement, "ON CONFLICT") != (i == 0 || i == 1 || i == 5) {
			t.Fatal("native per-table conflict policy changed")
		}
	}
	if len(tx.args[0]) != 20 || !reflect.DeepEqual(tx.args[1][2], []pgtype.UUID{{}}) || !reflect.DeepEqual(tx.args[1][4], []*int32{&muted}) || !reflect.DeepEqual(tx.args[1][8], []bool{true}) || !reflect.DeepEqual(tx.args[1][10], []string{"raw role"}) || !reflect.DeepEqual(tx.args[3][2], []*int32{&cipher}) || !reflect.DeepEqual(tx.args[4][4], []int32{-1}) || !reflect.DeepEqual(tx.args[4][5], []bool{true}) {
		t.Fatal("bound native arrays lost nullable UUID/status/MLS columns")
	}
	row.Members, row.Remotes, row.Subs, row.MLSClients = nil, nil, nil, nil
	tx = &observedUserSQL{}
	if err := writeConversationMigration(t.Context(), tx, row); err != nil {
		t.Fatal(err)
	}
	for _, args := range tx.args[1:5] {
		for _, arg := range args {
			if reflect.ValueOf(arg).IsNil() {
				t.Fatal("empty native vector became NULL array")
			}
		}
	}
}

func TestConversationMigrationFailureOrderingAndAbsentRecordCompletion(t *testing.T) {
	s := testMigrationService(time.Second)
	id, _ := postgresUUID(jobUUID)
	for _, failure := range []string{"none", "absent", "read", "commit", "cleanup", "complete"} {
		var calls []string
		inside := false
		step := func(ctx context.Context, name string) error {
			if _, timed := ctx.Deadline(); !timed || !inside {
				t.Fatal("local completion escaped lock/row timeout")
			}
			calls = append(calls, name)
			if failure == name {
				return errors.New("synthetic failure")
			}
			return nil
		}
		counted, err := s.copyConversationMigration(t.Context(), func(_ context.Context, action func() error) error {
			inside = true
			defer func() { inside = false }()
			return action()
		}, func(ctx context.Context) (*conversationMigrationRecord, error) {
			err := step(ctx, "read")
			if failure == "absent" {
				return nil, err
			}
			return &conversationMigrationRecord{ID: id}, err
		}, func(ctx context.Context, _ conversationMigrationRecord) error { return step(ctx, "commit") },
			func(ctx context.Context, _ conversationMigrationRecord) error { return step(ctx, "cleanup") },
			func(ctx context.Context) error { return step(ctx, "complete") })
		want := []string{"read", "commit", "cleanup", "complete"}
		if failure == "absent" {
			want = []string{"read", "complete"}
		} else if failure != "none" {
			for i, name := range want {
				if name == failure {
					want = want[:i+1]
					break
				}
			}
		}
		success := failure == "none" || failure == "absent"
		if !reflect.DeepEqual(calls, want) || counted != success || (err == nil) != success {
			t.Fatal("native local failure/absent count or pending ledger completion changed")
		}
	}
}
