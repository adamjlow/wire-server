// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestRemoteMembershipStatusDecodingPreservesNullableDefaultsAndDomainFold(t *testing.T) {
	id, _ := postgresUUID(jobUUID)
	row := migrationRow{"conv_remote_id": gocql.UUID(id.Bytes), "otr_muted_status": int32(-2147483648), "otr_muted_ref": "", "otr_archived": true, "hidden": nil}
	status, err := decodeRemoteConversationStatus("Remote.Example.COM", row)
	if err != nil || status.Domain != "remote.example.com" || status.Muted == nil || *status.Muted != -2147483648 || status.MutedRef == nil || *status.MutedRef != "" || !status.Archived || status.Hidden || status.ArchivedRef != nil || status.HiddenRef != nil {
		t.Fatal("native domain fold/nullable status defaults changed")
	}
	for column, value := range map[string]any{"conv_remote_id": nil, "otr_muted_status": int64(2147483648), "otr_muted_ref": []byte("private"), "otr_archived": 1, "hidden": "false"} {
		invalid := migrationRow{"conv_remote_id": gocql.UUID(id.Bytes)}
		invalid[column] = value
		if _, err := decodeRemoteConversationStatus("remote.example.com", invalid); err == nil || strings.Contains(err.Error(), "private") {
			t.Fatal("typed remote membership failure accepted or leaked values")
		}
	}
}

func TestRemoteMembershipScanUsesNativeCursorAndRefetchesCurrentStatuses(t *testing.T) {
	user, _ := postgresUUID(jobUUID)
	ids := make([]gocql.UUID, 1001)
	for i := range ids {
		ids[i][14], ids[i][15] = byte(i>>8), byte(i)
	}
	pages := 0
	query := func(_ context.Context, cql string, _ []string, size int, args ...any) ([]migrationRow, error) {
		if cql == remoteConversationStatuses {
			if pages != 2 || size != 0 || !reflect.DeepEqual(args, []any{gocql.UUID(user.Bytes), "remote.example.com", ids}) {
				t.Fatal("current status lookup changed source bound domain/id bucket")
			}
			// Disappearance after ID enumeration is native: copy only returned
			// statuses, and preserve changed status values from this reread.
			return []migrationRow{{"conv_remote_id": ids[1000], "hidden": true}, {"conv_remote_id": ids[0], "otr_archived": true}}, nil
		}
		pages++
		if size != 1001 {
			t.Fatal("source range lookahead page size lost")
		}
		if cql == remoteConversationIDs {
			if !reflect.DeepEqual(args, []any{gocql.UUID(user.Bytes)}) {
				t.Fatal("initial user query binding changed")
			}
			rows := make([]migrationRow, 1001)
			for i, id := range ids {
				rows[i] = migrationRow{"conv_remote_domain": "Remote.Example.COM", "conv_remote_id": id}
			}
			return rows, nil
		}
		if cql != remoteConversationIDsAfter || !reflect.DeepEqual(args, []any{gocql.UUID(user.Bytes), "remote.example.com", ids[999]}) {
			t.Fatal("keyset cursor used lookahead item or wrong domain/UUID")
		}
		return []migrationRow{{"conv_remote_domain": "remote.example.com", "conv_remote_id": ids[1000]}}, nil
	}
	rows, err := readRemoteConversationStatuses(t.Context(), user, query)
	if err != nil || len(rows) != 2 || rows[0].Conversation.Bytes != [16]byte(ids[0]) || !rows[0].Archived || rows[1].Conversation.Bytes != [16]byte(ids[1000]) || !rows[1].Hidden {
		t.Fatal("native status reread/map order changed")
	}
}

func TestRemoteMembershipMalformedLookaheadDoesNotCopyPartialPage(t *testing.T) {
	user, _ := postgresUUID(jobUUID)
	queries := 0
	query := func(context.Context, string, []string, int, ...any) ([]migrationRow, error) {
		queries++
		rows := make([]migrationRow, 1001)
		for i := range rows {
			rows[i] = migrationRow{"conv_remote_domain": "remote.example.com", "conv_remote_id": gocql.UUID(user.Bytes)}
		}
		rows[1000]["conv_remote_id"] = nil
		return rows, nil
	}
	if rows, err := readRemoteConversationStatuses(t.Context(), user, query); err == nil || rows != nil || queries != 1 {
		t.Fatal("malformed lookahead row hidden by page truncation")
	}
}

func TestRemoteMembershipFailurePreservesCopyCleanupAndLockCompletionOrder(t *testing.T) {
	s := testMigrationService(time.Second)
	for _, failure := range []string{"none", "read", "commit", "cleanup", "complete"} {
		var calls []string
		inside := false
		fail := errors.New("synthetic failure")
		lock := func(_ context.Context, action func() error) error {
			inside = true
			defer func() { inside = false }()
			return action()
		}
		step := func(ctx context.Context, name string) error {
			calls = append(calls, name)
			if inside == (name == "complete") {
				t.Fatal("source lock scope changed")
			}
			if _, timed := ctx.Deadline(); timed != (name != "complete") {
				t.Fatal("source timeout scope changed")
			}
			if failure == name {
				return fail
			}
			return nil
		}
		counted, err := s.copyRemoteConversationStatuses(t.Context(), lock,
			func(ctx context.Context) ([]remoteConversationStatus, error) { return nil, step(ctx, "read") },
			func(ctx context.Context, _ []remoteConversationStatus) error { return step(ctx, "commit") },
			func(ctx context.Context) error { return step(ctx, "cleanup") },
			func(ctx context.Context) error { return step(ctx, "complete") })
		want := []string{"read", "commit", "cleanup", "complete"}
		if failure != "none" {
			for i, name := range want {
				if name == failure {
					want = want[:i+1]
					break
				}
			}
		}
		if !reflect.DeepEqual(calls, want) || counted != (failure == "none") || (err != nil) != (failure != "none") {
			t.Fatal("failed copy/cleanup cleared ledger or counted success")
		}
	}
}

func TestRemoteMembershipSQLKeepsNativeConflictAndPendingLedgerBehavior(t *testing.T) {
	user, _ := postgresUUID(jobUUID)
	tx := &observedUserSQL{}
	muted := int32(0)
	if err := writeRemoteConversationStatuses(t.Context(), tx, user, []remoteConversationStatus{{Domain: "remote.example.com", Conversation: user, Muted: &muted}}); err != nil {
		t.Fatal(err)
	}
	if len(tx.statements) != 2 || strings.Contains(tx.statements[0], "ON CONFLICT") || !strings.Contains(tx.statements[1], "conversation_migration_pending_deletes") || len(tx.args[0]) != 9 || !reflect.DeepEqual(tx.args[0][3], []*int32{&muted}) || !reflect.DeepEqual(tx.args[0][5], []bool{false}) || !reflect.DeepEqual(tx.args[0][8], []*string{nil}) {
		t.Fatal("membership conflict semantics or nullable UNNEST columns changed")
	}
	if err := writeRemoteConversationStatuses(t.Context(), &observedUserSQL{}, user, nil); err != nil {
		t.Fatal("source empty membership partition failed to create pending ledger")
	}
}

func TestRemoteMembershipDomainQueriesUseNativeEightWorkerBound(t *testing.T) {
	user, _ := postgresUUID(jobUUID)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	started := make(chan struct{}, 9)
	release := make(chan struct{})
	var mu sync.Mutex
	active, maximum := 0, 0
	query := func(ctx context.Context, cql string, _ []string, _ int, _ ...any) ([]migrationRow, error) {
		if cql == remoteConversationIDs {
			rows := make([]migrationRow, 9)
			for i := range rows {
				rows[i] = migrationRow{"conv_remote_domain": string(rune('a'+i)) + ".example.com", "conv_remote_id": gocql.UUID(user.Bytes)}
			}
			return rows, nil
		}
		mu.Lock()
		active++
		maximum = max(maximum, active)
		mu.Unlock()
		defer func() { mu.Lock(); active--; mu.Unlock() }()
		started <- struct{}{}
		select {
		case <-release:
			return nil, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	done := make(chan error, 1)
	go func() {
		_, err := readRemoteConversationStatuses(ctx, pgtype.UUID{Bytes: user.Bytes, Valid: true}, query)
		done <- err
	}()
	for range 8 {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("source domain pool did not start eight queries")
		}
	}
	select {
	case <-started:
		t.Fatal("more than eight domain queries started")
	default:
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if maximum != 8 || active != 0 {
		t.Fatal("domain pool did not join or exceeded native bound")
	}
}
