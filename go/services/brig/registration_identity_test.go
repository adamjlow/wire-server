// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func TestRegistrationIdentityOwnershipAndBlockListOrder(t *testing.T) {
	owner := gocql.UUID{}
	email := "synthetic@example.com"
	deleted := int32(2)
	for _, scenario := range []struct {
		name    string
		owner   *gocql.UUID
		account *storedUser
		blocked bool
		want    []string
		err     error
	}{
		{name: "unused", want: []string{"owner", "blocklist"}},
		{name: "missing", owner: &owner, want: []string{"owner", "account", "delete", "blocklist"}},
		{name: "deleted", owner: &owner, account: &storedUser{Activated: true, Email: &email, Status: &deleted}, want: []string{"owner", "account", "delete", "blocklist"}},
		{name: "unactivated", owner: &owner, account: &storedUser{Email: &email}, want: []string{"owner", "account", "blocklist"}},
		{name: "activated-without-identity", owner: &owner, account: &storedUser{Activated: true}, want: []string{"owner", "account", "blocklist"}},
		{name: "occupied", owner: &owner, account: &storedUser{Activated: true, Email: &email}, want: []string{"owner", "account"}, err: errActivationKeyExists},
		{name: "blocked", owner: &owner, blocked: true, want: []string{"owner", "account", "delete", "blocklist"}, err: errRegistrationBlacklisted},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var calls []string
			service := registrationIdentity{
				owner: func(context.Context, string) (*gocql.UUID, error) {
					calls = append(calls, "owner")
					return scenario.owner, nil
				},
				account: func(context.Context, gocql.UUID) (*storedUser, error) {
					calls = append(calls, "account")
					return scenario.account, nil
				},
				deleteKey: func(_ context.Context, _ string, id *gocql.UUID) error {
					calls = append(calls, "delete")
					if id == nil || *id != owner {
						t.Fatal("stale key deleted without ownership guard")
					}
					return nil
				},
				blacklisted: func(context.Context, string) (bool, error) {
					calls = append(calls, "blocklist")
					return scenario.blocked, nil
				},
			}
			if err := service.verify(context.Background(), email); !errors.Is(err, scenario.err) || !reflect.DeepEqual(calls, scenario.want) {
				t.Fatal("registration identity order changed", err, calls)
			}
		})
	}
}
