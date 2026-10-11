// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func deletionTestService(user storedUser, steps *[]string, fail string) accountDeletionService {
	effect := func(name string) error {
		*steps = append(*steps, name)
		if name == fail {
			return errors.New("synthetic interrupted deletion")
		}
		return nil
	}
	one := func(name string) func(context.Context, gocql.UUID) error {
		return func(context.Context, gocql.UUID) error { return effect(name) }
	}
	return accountDeletionService{
		get:   func(context.Context, gocql.UUID) (*storedUser, error) { return &user, nil },
		owner: func(context.Context, gocql.UUID, gocql.UUID, string) (bool, error) { return false, effect("owner") },
		password: func(context.Context, gocql.UUID) (*string, error) {
			p := "synthetic-stored-hash"
			return &p, effect("password")
		},
		verify:   func(context.Context, gocql.UUID, string, string) (bool, error) { return true, effect("verify") },
		sendCode: func(context.Context, storedUser, string) (int64, error) { return 600, effect("code") },
		emailKey: func(_ context.Context, _ string, id *gocql.UUID) error {
			if id == nil || *id != user.ID {
				panic("unguarded email key removal")
			}
			return effect("email")
		},
		properties: one("properties"), tombstone: func(context.Context, storedUser) error { return effect("tombstone") },
		invitations: func(context.Context, storedUser) error { return effect("invitations") },
		groups:      func(context.Context, gocql.UUID, gocql.UUID, string) error { return effect("groups") },
		peers:       func(context.Context, storedUser, string) error { return effect("peers") },
		clients: func(context.Context, gocql.UUID) ([]storedClient, error) {
			return []storedClient{{ID: "a"}, {ID: "b"}}, effect("clients")
		},
		removeClient: func(_ context.Context, _ gocql.UUID, id string) error { return effect("client-" + id) },
		index:        func(context.Context, gocql.UUID, string) error { return effect("index") },
		events:       func(context.Context, gocql.UUID, string) error { return effect("events") }, connections: one("connections"), cookies: one("cookies"),
	}
}

func TestAccountDeletionAdmissionDoesNotMutate(t *testing.T) {
	ctx := context.Background()
	id, team := gocql.TimeUUID(), gocql.TimeUUID()
	email, password := "synthetic@example.com", "Synthetic-password"
	user := storedUser{ID: id, Team: &team, Email: &email, Activated: true}
	steps := []string{}
	s := deletionTestService(user, &steps, "")
	s.owner = func(context.Context, gocql.UUID, gocql.UUID, string) (bool, error) {
		steps = append(steps, "owner")
		return true, nil
	}
	if _, err := s.self(ctx, id, &password, "r"); !errors.Is(err, errDeleteUserOwner) || !reflect.DeepEqual(steps, []string{"owner"}) {
		t.Fatal("owner deletion bypass", steps, err)
	}
	user.Team = nil
	steps = nil
	s = deletionTestService(user, &steps, "")
	s.verify = func(context.Context, gocql.UUID, string, string) (bool, error) {
		steps = append(steps, "verify")
		return false, nil
	}
	if _, err := s.self(ctx, id, &password, "r"); !errors.Is(err, authInvalidCredentials) || !reflect.DeepEqual(steps, []string{"password", "verify"}) {
		t.Fatal("wrong password changed account", steps, err)
	}
	steps = nil
	s = deletionTestService(user, &steps, "")
	ttl, err := s.self(ctx, id, nil, "r")
	if err != nil || ttl == nil || *ttl != 600 || !reflect.DeepEqual(steps, []string{"code"}) {
		t.Fatal("verified email skipped code", steps, err)
	}
	user.Activated = false
	steps = nil
	s = deletionTestService(user, &steps, "")
	if _, err = s.self(ctx, id, nil, "r"); err != nil {
		t.Fatal(err)
	}
	for _, step := range steps {
		if step == "email" || step == "code" {
			t.Fatal("unvalidated identity used", steps)
		}
	}
	status := int32(2)
	user.Status = &status
	steps = nil
	s = deletionTestService(user, &steps, "")
	if _, err = s.self(ctx, id, &password, "r"); err != nil || len(steps) != 0 {
		t.Fatal("public deleted account repeated cleanup", steps, err)
	}
	s.get = func(context.Context, gocql.UUID) (*storedUser, error) { return nil, nil }
	if _, err = s.self(ctx, id, nil, "r"); !errors.Is(err, errDeleteUserInvalid) {
		t.Fatal("missing account succeeds", err)
	}
}

func TestAccountDeletionStopsAtFailedPhaseAndReplayKeepsOrder(t *testing.T) {
	ctx := context.Background()
	id, team := gocql.TimeUUID(), gocql.TimeUUID()
	email := "synthetic@example.com"
	user := storedUser{ID: id, Team: &team, Email: &email, Activated: true}
	want := []string{"email", "properties", "tombstone", "invitations", "groups", "peers", "clients", "client-a", "client-b", "index", "events", "connections", "cookies"}
	for index, phase := range want {
		steps := []string{}
		s := deletionTestService(user, &steps, phase)
		if err := s.execute(ctx, user, "r"); err == nil || !reflect.DeepEqual(steps, want[:index+1]) {
			t.Fatalf("phase %s advanced after failure: %v", phase, steps)
		}
	}
	// Queue replay must finish downstream cleanup even when its account is
	// already tombstoned. Public self deletion has the separate early return.
	deleted := int32(2)
	user.Status = &deleted
	steps := []string{}
	s := deletionTestService(user, &steps, "")
	if err := s.execute(ctx, user, "r"); err != nil || !reflect.DeepEqual(steps, want) {
		t.Fatal("replay skipped cleanup", steps, err)
	}
	steps = nil
	s.cookies = nil
	if err := s.execute(ctx, user, "r"); err == nil || len(steps) != 0 {
		t.Fatal("missing dependency allowed partial deletion", steps, err)
	}
}

func TestSuspendedAccountDeletionUsesPasswordWithoutReauthenticationStatus(t *testing.T) {
	suspended := int32(1)
	user := storedUser{ID: gocql.TimeUUID(), Status: &suspended}
	password := "Synthetic-password"
	steps := []string{}
	s := deletionTestService(user, &steps, "")
	if _, err := s.self(context.Background(), user.ID, &password, "r"); err != nil || len(steps) < 2 || steps[0] != "password" || steps[1] != "verify" {
		t.Fatal("suspended native deletion changed", steps, err)
	}
}
