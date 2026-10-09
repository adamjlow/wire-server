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
)

func TestUserMigrationRelatedReadsPreserveClaimsBotTeamAndEmptyAssets(t *testing.T) {
	id, _ := postgresUUID(jobUUID)
	other := id
	other.Bytes[15] ^= 1
	for _, ownership := range []string{"same", "other", "absent"} {
		t.Run(ownership, func(t *testing.T) {
			row := syntheticUserRow()
			row["provider"], row["service"] = gocql.UUID(id.Bytes), gocql.UUID(other.Bytes)
			row["handle"] = "synthetic'bound_handle"
			row["assets"] = []userCassandraAsset{}
			row["sso_id"] = `{"scim_external_id":"synthetic"}`
			var calls []string
			query := func(_ context.Context, cql string, _ []string, args ...any) (migrationRow, error) {
				calls = append(calls, cql)
				if strings.Contains(cql, "synthetic") {
					t.Fatal("stored handle interpolated in CQL")
				}
				switch cql {
				case userMigrationSelect:
					if !reflect.DeepEqual(args, []any{gocql.UUID(id.Bytes)}) {
						t.Fatal("wrong user binding")
					}
					return row, nil
				case userServiceConversationSelect:
					if !reflect.DeepEqual(args, []any{gocql.UUID(id.Bytes), gocql.UUID(other.Bytes), gocql.UUID(id.Bytes)}) {
						t.Fatal("wrong provider/service/user bindings")
					}
					return migrationRow{"conv": gocql.UUID(other.Bytes), "team": gocql.UUID(id.Bytes)}, nil
				case userHandleClaimSelect:
					if !reflect.DeepEqual(args, []any{row["handle"]}) {
						t.Fatal("wrong handle binding")
					}
					if ownership == "absent" {
						return nil, nil
					}
					owner := id
					if ownership == "other" {
						owner = other
					}
					return migrationRow{"user": gocql.UUID(owner.Bytes)}, nil
				case userRichInfoSelect:
					return migrationRow{"json": []byte(`{"version":0,"fields":[{"type":"synthetic","value":"value"}]}`)}, nil
				default:
					return nil, errors.New("unexpected synthetic query")
				}
			}
			record, err := readUserMigration(t.Context(), id, query)
			if err != nil || record == nil {
				t.Fatalf("related user read failed: %v", err)
			}
			want := []string{userMigrationSelect, userServiceConversationSelect, userHandleClaimSelect, userRichInfoSelect}
			if !reflect.DeepEqual(calls, want) || (record.Handle != nil) != (ownership == "same") || record.Assets == nil || len(record.Assets) != 0 || record.ServiceConversation == nil || record.ServiceConversation.Conversation != other || record.ServiceConversation.Team != id || record.RichInfo == nil || record.SSO == nil {
				t.Fatal("native related-read order/claim retention/nullable asset behavior changed")
			}
			for _, statement := range userMigrationDeletes(*record) {
				if strings.Contains(statement.CQL, "user_handle") && ownership != "same" {
					t.Fatal("cleanup would delete another user's or unclaimed handle")
				}
			}
		})
	}
}

func TestUserMigrationTypedRelatedErrorsAreNotHiddenByIneligibleUser(t *testing.T) {
	id, _ := postgresUUID(jobUUID)
	for _, malformed := range []string{"service", "claim", "rich", "none"} {
		row := syntheticUserRow()
		row["name"] = nil
		row["provider"], row["service"], row["handle"] = gocql.UUID(id.Bytes), gocql.UUID(id.Bytes), "synthetic"
		var calls []string
		query := func(_ context.Context, cql string, _ []string, _ ...any) (migrationRow, error) {
			calls = append(calls, cql)
			switch cql {
			case userMigrationSelect:
				return row, nil
			case userServiceConversationSelect:
				if malformed == "service" {
					return migrationRow{"conv": nil}, nil
				}
				return nil, nil
			case userHandleClaimSelect:
				if malformed == "claim" {
					return migrationRow{"user": nil}, nil
				}
				return nil, nil
			case userRichInfoSelect:
				if malformed == "rich" {
					return migrationRow{"json": []byte("synthetic malformed JSON")}, nil
				}
				return nil, nil
			default:
				return nil, errors.New("unexpected query")
			}
		}
		record, err := readUserMigration(t.Context(), id, query)
		if record != nil || (err != nil) != (malformed != "none") {
			t.Fatal("missing name masked a related typed error or copied invalid user")
		}
		if malformed == "none" && len(calls) != 4 {
			t.Fatal("native eligibility skip occurred before related reads")
		}
	}
	row := syntheticUserRow()
	row["name"], row["sso_id"] = nil, `{"scim_external_id":123}`
	if _, _, err := decodeUserMigrationRow(id, row); err == nil {
		t.Fatal("missing-name skip hid malformed SSO")
	}
}

func TestUserMigrationLockWrapsReadCommitCleanupAndCompletion(t *testing.T) {
	s := testMigrationService(time.Second)
	id, _ := postgresUUID(jobUUID)
	for _, absent := range []bool{false, true} {
		store := &observedUserMigrationStore{}
		inside := false
		lock := func(_ context.Context, action func() error) error {
			inside = true
			defer func() { inside = false }()
			return action()
		}
		read := func(ctx context.Context) (*userMigrationRecord, error) {
			if !inside {
				t.Fatal("user reread occurred outside exclusive lock")
			}
			if _, hasDeadline := ctx.Deadline(); !hasDeadline {
				t.Fatal("row timeout not installed after lock acquisition")
			}
			if absent {
				return nil, nil
			}
			record := syntheticUserMigrationRecord()
			return &record, nil
		}
		counted, err := s.copyLockedUserMigration(t.Context(), id, lock, read, store, func(context.Context, userMigrationRecord) error {
			if !inside || !reflect.DeepEqual(store.calls, []string{"commit-copy-and-pending"}) {
				t.Fatal("CQL cleanup before PG commit or outside lock")
			}
			store.calls = append(store.calls, "cleanup")
			return nil
		})
		if err != nil || counted == absent || inside {
			t.Fatal("locked user migration counting/lifetime changed")
		}
		if absent && len(store.calls) != 0 {
			t.Fatal("source-disappeared user mutated PG or cleared pending ledger")
		}
		if !absent && !reflect.DeepEqual(store.calls, []string{"commit-copy-and-pending", "cleanup", "complete-pending"}) {
			t.Fatal("copy/cleanup/complete order changed")
		}
	}
}
