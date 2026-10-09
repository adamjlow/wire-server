// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type observedConversationStore struct {
	calls *[]string
	name  string
	conv  *storedConversation
}

func (s observedConversationStore) Conversation(context.Context, string) (*storedConversation, error) {
	*s.calls = append(*s.calls, s.name+"-read")
	return s.conv, nil
}

func (s observedConversationStore) UpsertLocalMembers(context.Context, string, []string, string) error {
	*s.calls = append(*s.calls, s.name+"-write")
	return nil
}

type observedMigrationStore struct {
	observedConversationStore
	exists     bool
	cleanupErr error
}

func (s observedMigrationStore) WithConversationLock(_ context.Context, _ string, exclusive bool, action func() error) error {
	if exclusive {
		return errors.New("ordinary operation requested exclusive lock")
	}
	*s.calls = append(*s.calls, "lock")
	err := action()
	*s.calls = append(*s.calls, "unlock")
	return err
}

func (s observedMigrationStore) CleanupConversation(context.Context, string) error {
	*s.calls = append(*s.calls, "cleanup")
	return s.cleanupErr
}

func (s observedMigrationStore) ConversationExists(context.Context, string) (bool, error) {
	*s.calls = append(*s.calls, "exists")
	return s.exists, nil
}

func TestConversationRoutingMatchesSourceStorageModes(t *testing.T) {
	for _, test := range []struct {
		name     string
		location storageLocation
		pgConv   bool
		write    bool
		pgExists bool
		calls    []string
	}{
		{"PG-read", storagePostgres, true, false, true, []string{"pg-read"}},
		{"Cass-read", storageCassandra, false, false, false, []string{"cass-read"}},
		{"migration-PG-read", storageMigrating, true, false, true, []string{"lock", "cleanup", "pg-read", "unlock"}},
		{"migration-fallback", storageMigrating, false, false, false, []string{"lock", "cleanup", "pg-read", "cass-read", "unlock"}},
		{"migration-incomplete-MLS-write", storageMigrating, false, true, true, []string{"lock", "cleanup", "exists", "pg-write", "unlock"}},
		{"migration-Cass-write", storageMigrating, false, true, false, []string{"lock", "cleanup", "exists", "cass-write", "unlock"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls []string
			pg := observedMigrationStore{observedConversationStore: observedConversationStore{calls: &calls, name: "pg"}, exists: test.pgExists}
			if test.pgConv {
				pg.conv = &storedConversation{ID: jobUUID}
			}
			store := routedGroupStore{Postgres: pg, Cassandra: observedConversationStore{calls: &calls, name: "cass"}, Location: test.location}
			var err error
			if test.write {
				err = store.UpsertLocalMembers(t.Context(), jobUUID, []string{jobUUID}, "wire_member")
			} else {
				_, err = store.Conversation(t.Context(), jobUUID)
			}
			if err != nil || !reflect.DeepEqual(calls, test.calls) {
				t.Fatalf("calls=%v err=%v", calls, err)
			}
		})
	}
}

func TestConversationRoutingStopsOnFailedPendingCleanup(t *testing.T) {
	var calls []string
	failure := errors.New("synthetic deletion failed")
	pg := observedMigrationStore{observedConversationStore: observedConversationStore{calls: &calls, name: "pg"}, cleanupErr: failure}
	s := routedGroupStore{Postgres: pg, Cassandra: observedConversationStore{calls: &calls, name: "cass"}, Location: storageMigrating}
	if _, err := s.Conversation(t.Context(), jobUUID); !errors.Is(err, failure) || !reflect.DeepEqual(calls, []string{"lock", "cleanup", "unlock"}) {
		t.Fatal("failed deletion proceeded to a stale read")
	}
	calls = nil
	if err := s.UpsertLocalMembers(t.Context(), jobUUID, nil, "wire_member"); !errors.Is(err, failure) || !reflect.DeepEqual(calls, []string{"lock", "cleanup", "unlock"}) {
		t.Fatal("failed deletion proceeded to a member write")
	}
}

type failingCassandraCleanup struct {
	failure error
	calls   *int
}

func (s failingCassandraCleanup) CleanupMigratedConversation(context.Context, string) error {
	*s.calls++
	return s.failure
}

func TestPendingDeleteLedgerRetainsFailedCleanupAndCompletesAfterSuccess(t *testing.T) {
	var calls, completions int
	failure := errors.New("synthetic cleanup failed")
	mark := func() error { completions++; return nil }
	if err := completePendingConversationDelete(t.Context(), jobUUID, false, nil, mark); err != nil || completions != 0 {
		t.Fatal("absent ledger caused work")
	}
	if err := completePendingConversationDelete(t.Context(), jobUUID, true, nil, mark); err == nil || completions != 0 {
		t.Fatal("unavailable cleanup silently completed ledger")
	}
	if err := completePendingConversationDelete(t.Context(), jobUUID, true, failingCassandraCleanup{failure, &calls}, mark); !errors.Is(err, failure) || completions != 0 || calls != 1 {
		t.Fatal("failed cleanup was acknowledged")
	}
	if err := completePendingConversationDelete(t.Context(), jobUUID, true, failingCassandraCleanup{nil, &calls}, mark); err != nil || completions != 1 || calls != 2 {
		t.Fatal("successful cleanup did not complete ledger")
	}
}

func TestMigrationUUIDLockKeysMatchNativeWordMix(t *testing.T) {
	for _, test := range []struct {
		id  string
		key int64
	}{
		{"00000000-0000-0000-0000-000000000001", 1 << 32},
		{"00000000-0000-0001-0000-000000000000", 1},
		{"ffffffff-ffff-ffff-0000-000000000000", -1},
	} {
		key, err := uuidMigrationLockKey(test.id)
		if err != nil || key != test.key {
			t.Fatal("native advisory lock key mismatch")
		}
	}
	if _, err := uuidMigrationLockKey("invalid;SELECT"); err == nil {
		t.Fatal("invalid migration lock identifier accepted")
	}
}
