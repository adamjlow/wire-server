// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testMigrationService(timeout time.Duration) *Service {
	c := Config{MigrationOptions: migrationOptions{PageSize: 2, Parallelism: 2, Timeout: Duration(timeout)}}
	return New(c, GalleyConfig{})
}

func TestMigrationRescansAfterPGUsageFailureAndKeepsSuccessfulCopies(t *testing.T) {
	s := testMigrationService(time.Second)
	pages, writes := 0, 0
	page := func(ctx context.Context, state []byte, size int) ([]migrationRow, []byte, error) {
		pages++
		if size != 2 {
			t.Fatal("configured source page size lost")
		}
		if len(state) == 0 {
			return []migrationRow{{"key": "one"}}, []byte{1}, nil
		}
		return []migrationRow{{"key": "two"}}, nil, nil
	}
	write := func(context.Context, migrationRow) (bool, error) {
		writes++
		if writes == 1 {
			return false, &postgresOperationError{message: "synthetic usage failure"}
		}
		return true, nil
	}
	if err := s.migrateRows(t.Context(), "block_list", page, write); err != nil {
		t.Fatal(err)
	}
	m := s.migrations["block_list"]
	if pages != 4 || writes != 4 || m.migrated != 3 || m.finished != 1 || m.failed != 0 || m.observations["error"].count != 1 {
		t.Fatal("native full-rescan/counting behavior changed")
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/i/metrics", nil))
	if !strings.Contains(w.Body.String(), "wire_block_list_migrated_to_pg 3.0\n") || !strings.Contains(w.Body.String(), "wire_block_list_migration_finished 1.0\n") {
		t.Fatal("source migration counters missing")
	}
}

func TestMigrationTimeoutStopsUntilRestart(t *testing.T) {
	s := testMigrationService(10 * time.Millisecond)
	pages := 0
	page := func(context.Context, []byte, int) ([]migrationRow, []byte, error) {
		pages++
		return []migrationRow{{}}, nil, nil
	}
	write := func(ctx context.Context, _ migrationRow) (bool, error) { <-ctx.Done(); return false, ctx.Err() }
	if err := s.migrateRows(t.Context(), "password_reset", page, write); err == nil {
		t.Fatal("timeout accepted")
	}
	m := s.migrations["password_reset"]
	if pages != 1 || m.failed != 1 || m.finished != 0 || m.migrated != 0 || m.observations["timeout"].count != 1 {
		t.Fatal("timeout restarted or counted success")
	}
}

func TestMigrationValidatesEntirePageBeforeAnyCopy(t *testing.T) {
	s := testMigrationService(time.Second)
	writes := 0
	page := validatedMigrationPage(func(context.Context, []byte, int) ([]migrationRow, []byte, error) {
		return []migrationRow{{"valid": true}, {"valid": false}}, nil, nil
	}, func(row migrationRow) error {
		if row["valid"] != true {
			return errors.New("synthetic malformed stored row")
		}
		return nil
	})
	if err := s.migrateRows(t.Context(), "block_list", page, func(context.Context, migrationRow) (bool, error) { writes++; return true, nil }); err == nil || writes != 0 {
		t.Fatal("partially copied a malformed native page")
	}
	if s.migrations["block_list"].failed != 1 {
		t.Fatal("fatal page error counted more than once")
	}
}
