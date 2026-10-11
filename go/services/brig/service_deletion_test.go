// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func TestServiceBotDeletionNativeOrderAndFailures(t *testing.T) {
	for _, fail := range []string{"", "member", "account", "clients", "client-a", "client-b", "association", "status"} {
		t.Run(fail, func(t *testing.T) {
			var calls []string
			failure := errors.New("synthetic failure")
			step := func(name string) error {
				calls = append(calls, name)
				if name == fail {
					return failure
				}
				return nil
			}
			provider, service, user := gocql.TimeUUID(), gocql.TimeUUID(), gocql.TimeUUID()
			s := serviceBotDeletion{
				removeMember: func(context.Context, deletionServiceBot) error { return step("member") },
				account: func(context.Context, gocql.UUID) (*storedUser, error) {
					return &storedUser{Provider: &provider, Service: &service}, step("account")
				},
				clients: func(context.Context, gocql.UUID) ([]storedClient, error) {
					return []storedClient{{ID: "a"}, {ID: "b"}}, step("clients")
				},
				removeClient: func(_ context.Context, _ gocql.UUID, id string) error { return step("client-" + id) },
				removeAssociation: func(_ context.Context, p, s, u gocql.UUID) error {
					if p != provider || s != service || u != user {
						t.Fatal("wrong association")
					}
					return step("association")
				},
				status: func(_ context.Context, u gocql.UUID, status int32) error {
					if u != user || status != 2 {
						t.Fatal("wrong tombstone status")
					}
					return step("status")
				},
			}
			err := s.execute(context.Background(), deletionServiceBot{user: user})
			want := []string{"member", "account", "clients", "client-a", "client-b", "association", "status"}
			if fail != "" {
				for i, name := range want {
					if name == fail {
						want = want[:i+1]
						break
					}
				}
				if !errors.Is(err, failure) {
					t.Fatal("failure lost")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(calls, want) {
				t.Fatalf("calls %v want %v", calls, want)
			}
		})
	}
}

func TestServiceDeletionBoundedWorkersAndFailedPageRetainsMetadata(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	entered := make(chan struct{}, 17)
	release := make(chan struct{})
	done := make(chan error, 1)
	var active, peak, completed atomic.Int32
	final := false
	s := serviceDeletion{
		lookup: func(context.Context, gocql.UUID, gocql.UUID) (*deletionServiceRecord, error) {
			return &deletionServiceRecord{}, nil
		},
		pages: func(_ context.Context, _, _ gocql.UUID, visit func([]deletionServiceBot) error) error {
			return visit(make([]deletionServiceBot, 17))
		},
		kick: func(ctx context.Context, _ deletionServiceBot) error {
			n := active.Add(1)
			defer active.Add(-1)
			for old := peak.Load(); n > old; old = peak.Load() {
				if peak.CompareAndSwap(old, n) {
					break
				}
			}
			entered <- struct{}{}
			select {
			case <-release:
				completed.Add(1)
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
		disconnect: func(context.Context, gocql.UUID, gocql.UUID) error {
			if completed.Load() != 17 {
				t.Error("disconnect before bots")
			}
			return nil
		},
		remove: func(context.Context, gocql.UUID, gocql.UUID, deletionServiceRecord) error { final = true; return nil },
	}
	go func() { done <- s.execute(ctx, gocql.UUID{}, gocql.UUID{}) }()
	for i := 0; i < 16; i++ {
		select {
		case <-entered:
		case <-ctx.Done():
			close(release)
			t.Fatal("workers did not saturate")
		}
	}
	if active.Load() != 16 {
		t.Error("unexpected saturation")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if peak.Load() != 16 || completed.Load() != 17 || !final {
		t.Fatal("bounded completion missing")
	}
	final = false
	s.kick = func(context.Context, deletionServiceBot) error { return errors.New("failed bot") }
	s.disconnect = func(context.Context, gocql.UUID, gocql.UUID) error {
		t.Error("disconnect after bot failure")
		return nil
	}
	if s.execute(ctx, gocql.UUID{}, gocql.UUID{}) == nil || final {
		t.Fatal("failed page deleted metadata")
	}
	// Missing service is a successful replay without requiring other adapters.
	missing := serviceDeletion{lookup: func(context.Context, gocql.UUID, gocql.UUID) (*deletionServiceRecord, error) { return nil, nil }}
	if err := missing.execute(ctx, gocql.UUID{}, gocql.UUID{}); err != nil {
		t.Fatal(err)
	}
}
