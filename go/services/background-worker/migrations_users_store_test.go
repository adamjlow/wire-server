// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

type observedUserSQL struct {
	statements  []string
	args        [][]any
	taken       bool
	failPending bool
}
type observedUserScan struct{ taken bool }

func (r observedUserScan) Scan(dest ...any) error { *dest[0].(*bool) = r.taken; return nil }
func (s *observedUserSQL) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	s.statements = append(s.statements, sql)
	s.args = append(s.args, args)
	return observedUserScan{s.taken}
}
func (s *observedUserSQL) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	s.statements = append(s.statements, sql)
	s.args = append(s.args, args)
	if s.failPending && strings.Contains(sql, "user_migration_pending_deletes") {
		return pgconn.CommandTag{}, errors.New("synthetic private database diagnostic")
	}
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

func syntheticUserMigrationRecord() userMigrationRecord {
	id, _ := postgresUUID(jobUUID)
	handle := "synthetic_handle"
	password := "synthetic password encoding"
	return userMigrationRecord{ID: id, Handle: &handle, Password: &password, Name: "Synthetic user", CreatedAt: time.Unix(1700000000, 0).UTC()}
}

func TestUserMigrationSQLBranchesPreserveDeletedUserBotAssetsAndPendingLedger(t *testing.T) {
	deleted := int32(2)
	for _, variant := range []string{"regular", "deleted-regular", "deleted-bot", "bot", "empty-assets"} {
		row := syntheticUserMigrationRecord()
		if strings.HasPrefix(variant, "deleted") {
			row.Status = &deleted
		}
		if strings.Contains(variant, "bot") {
			row.UserType = 1
			row.ServiceConversation = &userMigrationServiceConversation{Conversation: row.ID}
		}
		if variant == "empty-assets" {
			row.Assets = []userMigrationAsset{}
		}
		tx := &observedUserSQL{}
		if _, err := writeUserMigration(t.Context(), tx, row); err != nil {
			t.Fatal(err)
		}
		all := strings.Join(tx.statements, "\n")
		if strings.Contains(all, "INSERT INTO deleted_user") != (variant == "deleted-regular") || strings.Contains(all, "INSERT INTO wire_user") == (variant == "deleted-regular") || strings.Contains(all, "INSERT INTO bot_conv") != (variant == "bot") || strings.Contains(all, "DELETE FROM asset") != (variant == "empty-assets") {
			t.Fatalf("native user %s branch changed", variant)
		}
		if !strings.HasPrefix(tx.statements[len(tx.statements)-1], "INSERT INTO user_migration_pending_deletes") || strings.Contains(all, "synthetic") {
			t.Fatal("pending ledger not in transaction or user secrets interpolated")
		}
	}
}

func TestUserMigrationDuplicatePGHandleDoesNotChangeOriginalCleanupHandle(t *testing.T) {
	row := syntheticUserMigrationRecord()
	tx := &observedUserSQL{taken: true}
	removed, err := writeUserMigration(t.Context(), tx, row)
	if err != nil || !removed {
		t.Fatal("duplicate handle was not removed")
	}
	for i, sql := range tx.statements {
		if strings.HasPrefix(sql, "INSERT INTO wire_user") {
			args := tx.args[i]
			if len(args) != 25 || args[8].(*string) != nil || args[12] != row.Password {
				t.Fatal("native nullable handle/password tuple changed")
			}
		}
	}
	deletes := userMigrationDeletes(row)
	if deletes[0].Values[0] != "synthetic_handle" {
		t.Fatal("native original handle cleanup lost after duplicate PG claim")
	}
}

type observedUserMigrationStore struct {
	calls                    []string
	saveError, completeError error
}

func (s *observedUserMigrationStore) saveUserMigration(context.Context, userMigrationRecord) (bool, error) {
	s.calls = append(s.calls, "commit-copy-and-pending")
	return false, s.saveError
}
func (s *observedUserMigrationStore) completeUserMigration(context.Context, pgtype.UUID) error {
	s.calls = append(s.calls, "complete-pending")
	return s.completeError
}

func TestUserMigrationNeverDeletesBeforeCopyCommitAndKeepsPendingOnCleanupFailure(t *testing.T) {
	for _, variant := range []string{"success", "save-fails", "cleanup-fails", "complete-fails"} {
		store := &observedUserMigrationStore{}
		failure := errors.New("synthetic failure")
		if variant == "save-fails" {
			store.saveError = failure
		}
		if variant == "complete-fails" {
			store.completeError = failure
		}
		counted, err := copyUserMigration(t.Context(), syntheticUserMigrationRecord(), store, func(context.Context, userMigrationRecord) error {
			store.calls = append(store.calls, "cleanup-Cassandra")
			if variant == "cleanup-fails" {
				return failure
			}
			return nil
		})
		want := []string{"commit-copy-and-pending"}
		if variant != "save-fails" {
			want = append(want, "cleanup-Cassandra")
		}
		if variant != "save-fails" && variant != "cleanup-fails" {
			want = append(want, "complete-pending")
		}
		if !reflect.DeepEqual(store.calls, want) || counted != (variant == "success") || (err == nil) != (variant == "success") {
			t.Fatal("native durable copy/cleanup order changed")
		}
	}
	tx := &observedUserSQL{failPending: true}
	if _, err := writeUserMigration(t.Context(), tx, syntheticUserMigrationRecord()); err == nil || strings.Contains(err.Error(), "private") {
		t.Fatal("pending ledger failure accepted or raw diagnostics leaked")
	}
}

func TestUserMigrationCleanupPreservesNativeServiceTeamAndRichInfoSequence(t *testing.T) {
	row := syntheticUserMigrationRecord()
	row.Provider = row.ID
	row.Service = row.ID
	row.ServiceConversation = &userMigrationServiceConversation{Conversation: row.ID, Team: row.ID}
	deletes := userMigrationDeletes(row)
	want := []string{"user_handle", "service_user", "service_team", "rich_info", "user"}
	if len(deletes) != len(want) {
		t.Fatal("native cleanup relation missing")
	}
	for i, table := range want {
		if !strings.HasPrefix(deletes[i].CQL, "DELETE FROM "+table+" WHERE") || strings.Contains(deletes[i].CQL, "synthetic") {
			t.Fatal("native cleanup order or bound values changed")
		}
	}
}

func TestUserMigrationRetriesSerializableConflictsInsideTransactionUntilContextStops(t *testing.T) {
	calls := 0
	removed, err := retryUserMigrationTransaction(t.Context(), func() (bool, error) {
		calls++
		if calls == 1 {
			return false, &postgresOperationError{state: "40001"}
		}
		if calls == 2 {
			return false, &postgresOperationError{state: "40P01"}
		}
		return true, nil
	})
	if err != nil || !removed || calls != 3 {
		t.Fatal("native serialization/deadlock retry changed")
	}
	calls = 0
	_, err = retryUserMigrationTransaction(t.Context(), func() (bool, error) { calls++; return false, &postgresOperationError{state: "23505"} })
	if err == nil || calls != 1 {
		t.Fatal("non-retriable constraint error hidden")
	}
	ctx, cancel := context.WithCancel(t.Context())
	calls = 0
	_, err = retryUserMigrationTransaction(ctx, func() (bool, error) { calls++; cancel(); return false, &postgresOperationError{state: "40001"} })
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatal("transaction retry ignored cancellation")
	}
}
