// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestMigrationParallelPoolBoundsConcurrencyAndFinishesPageBeforeFetch(t *testing.T) {
	s := testMigrationService(time.Second)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	started := make(chan struct{}, 5)
	release := make(chan struct{})
	var active, maximum, completed atomic.Int32
	page := func(_ context.Context, state []byte, _ int) ([]migrationRow, []byte, error) {
		if len(state) != 0 {
			if active.Load() != 0 || completed.Load() != 4 {
				return nil, nil, errors.New("next page fetched before current page finished")
			}
			return []migrationRow{{}}, nil, nil
		}
		return []migrationRow{{}, {}, {}, {}}, []byte{1}, nil
	}
	write := func(ctx context.Context, _ migrationRow) (bool, error) {
		n := active.Add(1)
		defer active.Add(-1)
		for old := maximum.Load(); n > old; old = maximum.Load() {
			if maximum.CompareAndSwap(old, n) {
				break
			}
		}
		started <- struct{}{}
		select {
		case <-release:
			completed.Add(1)
			return true, nil
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
	done := make(chan error, 1)
	go func() {
		done <- s.migratePhases(ctx, "users", []migrationPhase{{page: page, write: write, parallel: true}})
	}()
	for range 2 {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("configured parallel workers did not start")
		}
	}
	select {
	case <-started:
		t.Fatal("more than configured workers started")
	default:
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if maximum.Load() != 2 || completed.Load() != 5 || s.migrations["users"].migrated != 5 || s.migrations["users"].finished != 1 {
		t.Fatal("bounded pool or per-page completion/counting changed")
	}
}

func TestMigrationParallelFatalErrorCancelsAndJoinsSiblings(t *testing.T) {
	s := testMigrationService(time.Second)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	ready, cleaned := make(chan struct{}), make(chan struct{})
	fatal := errors.New("synthetic malformed stored user")
	var writes atomic.Int32
	page := func(context.Context, []byte, int) ([]migrationRow, []byte, error) {
		return []migrationRow{{"id": 0}, {"id": 1}, {"id": 2}, {"id": 3}}, nil, nil
	}
	write := func(ctx context.Context, row migrationRow) (bool, error) {
		writes.Add(1)
		switch row["id"] {
		case 0:
			select {
			case <-ready:
				return false, fatal
			case <-ctx.Done():
				return false, ctx.Err()
			}
		case 1:
			defer close(cleaned)
			close(ready)
			<-ctx.Done()
			return false, ctx.Err()
		default:
			return true, nil
		}
	}
	err := s.migratePhases(ctx, "users", []migrationPhase{{page: page, write: write, parallel: true}})
	if !errors.Is(err, fatal) {
		t.Fatalf("fatal error lost: %v", err)
	}
	select {
	case <-cleaned:
	default:
		t.Fatal("migration returned while sibling cleanup was still running")
	}
	if writes.Load() != 2 || s.migrations["users"].failed != 1 || s.migrations["users"].finished != 0 || s.migrations["users"].migrated != 0 {
		t.Fatal("fatal cancellation started remaining rows or marked success")
	}
}

func TestMigrationParallelCaughtErrorsFinishPageAndRescan(t *testing.T) {
	s := testMigrationService(time.Second)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	pages := 0
	var writes atomic.Int32
	page := func(context.Context, []byte, int) ([]migrationRow, []byte, error) {
		pages++
		return []migrationRow{{"id": 0, "pass": pages}, {"id": 1, "pass": pages}, {"id": 2, "pass": pages}}, nil, nil
	}
	write := func(_ context.Context, row migrationRow) (bool, error) {
		writes.Add(1)
		if row["pass"] == 1 {
			switch row["id"] {
			case 0:
				return false, &postgresOperationError{message: "synthetic usage failure"}
			case 1:
				return false, &migrationLockTimeout{}
			}
		}
		return true, nil
	}
	if err := s.migratePhases(ctx, "users", []migrationPhase{{page: page, write: write, parallel: true}}); err != nil {
		t.Fatal(err)
	}
	m := s.migrations["users"]
	if pages != 2 || writes.Load() != 6 || m.migrated != 4 || m.finished != 1 || m.failed != 0 {
		t.Fatal("caught lock/PG errors cancelled siblings or did not rescan")
	}
}
