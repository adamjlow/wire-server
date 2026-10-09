// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"reflect"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func TestCassandraScalarNullsStayAbsent(t *testing.T) {
	for _, typ := range []gocql.Type{gocql.TypeInt, gocql.TypeUUID, gocql.TypeVarchar, gocql.TypeBoolean} {
		info := gocql.NewNativeType(4, typ, "")
		v := nullableCassandraValue{}
		if err := v.UnmarshalCQL(info, nil); err != nil || v.Value != nil {
			t.Fatal("CQL NULL scalar became a present zero value")
		}
	}
	info := gocql.NewNativeType(4, gocql.TypeInt, "")
	var v nullableCassandraValue
	if err := v.UnmarshalCQL(info, []byte{0, 0, 0, 0}); err != nil || v.Value != int(0) {
		t.Fatalf("present active status0 lost: %v, %v", v.Value, err)
	}
	info = gocql.NewNativeType(4, gocql.TypeVarchar, "")
	if err := v.UnmarshalCQL(info, []byte{}); err != nil || v.Value != "" {
		t.Fatal("present empty text became absent")
	}
}

func TestCassandraConversationPreservesLegacyAccessAndEpochWriteTime(t *testing.T) {
	for _, test := range []struct {
		legacy int
		roles  []int32
	}{{1, []int32{}}, {2, []int32{1}}, {3, []int32{1, 2, 4}}, {4, []int32{1, 2, 3, 4}}} {
		row, deleted, err := decodeCassandraConversation(map[string]any{"type": 0, "access_role": test.legacy, "writetime(epoch)": int64(1700000000123456)})
		if err != nil || deleted || !reflect.DeepEqual(row.AccessRoles, test.roles) || row.EpochTime == nil || !row.EpochTime.Equal(time.UnixMicro(1700000000123456)) {
			t.Fatalf("legacy %d: row=%+v err=%v", test.legacy, row, err)
		}
		if row.Protocol != nil || row.Creator.Valid || row.Name != nil {
			t.Fatal("missing fields became present zero values")
		}
	}
}

func TestCassandraConversationRejectsMalformedMetadata(t *testing.T) {
	for _, value := range []map[string]any{
		{}, {"type": 4}, {"type": 0, "protocol": 3}, {"type": 0, "history_depth": int64(-1)}, {"type": 0, "access_role": 0}, {"type": 0, "creator": "invalid-uuid"}, {"type": 0, "deleted": "true"}, {"type": 0, "access_roles_v2": []int32{9}},
	} {
		if _, _, err := decodeCassandraConversation(value); err == nil {
			t.Fatal("malformed conversation metadata accepted")
		}
	}
}
