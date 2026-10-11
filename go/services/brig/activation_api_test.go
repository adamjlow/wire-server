// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type apiCodeLookup struct {
	verifyCode func(context.Context, string, string) (*activationRecord, error)
}

func (s apiCodeLookup) verify(ctx context.Context, key, code string) (*activationRecord, error) {
	return s.verifyCode(ctx, key, code)
}

func TestActivationAPIOrdering(t *testing.T) {
	for _, scenario := range []struct {
		name                          string
		dryrun, verify, first, repeat bool
		fail                          string
		want                          []string
	}{
		{name: "first", verify: true, first: true, want: []string{"verify", "domain", "activate", "index", "event", "team"}},
		{name: "email", verify: true, want: []string{"verify", "domain", "activate", "index", "event", "clear"}},
		{name: "repeat", verify: true, repeat: true, want: []string{"verify", "domain", "activate"}},
		{name: "dryrun", dryrun: true, want: []string{"verify", "domain"}},
		{name: "internal", first: true, want: []string{"activate", "index", "event", "team"}},
		{name: "badcode", verify: true, fail: "verify", want: []string{"verify"}},
		{name: "blocked", verify: true, fail: "domain", want: []string{"verify", "domain"}},
		{name: "mutation", verify: true, fail: "activate", want: []string{"verify", "domain", "activate"}},
		{name: "indexfailure", verify: true, fail: "index", want: []string{"verify", "domain", "activate", "index"}},
		{name: "journalfailure", verify: true, fail: "event", want: []string{"verify", "domain", "activate", "index", "event"}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var calls []string
			failure := errors.New("synthetic failure")
			step := func(name string) error {
				calls = append(calls, name)
				if name == scenario.fail {
					return failure
				}
				return nil
			}
			result := &activatedIdentity{user: gocql.UUID{}, email: "synthetic@example.com", first: scenario.first}
			api := activationAPI{
				codes: apiCodeLookup{func(context.Context, string, string) (*activationRecord, error) {
					return &activationRecord{email: result.email}, step("verify")
				}},
				guardDomain: func(_ context.Context, email string) error {
					if email != result.email {
						t.Fatal("wrong verified identity")
					}
					return step("domain")
				},
				activate: func(context.Context, string, string, *gocql.UUID) (*activatedIdentity, error) {
					err := step("activate")
					if scenario.repeat {
						return nil, err
					}
					return result, err
				},
				syncIndex:        func(context.Context, gocql.UUID) error { return step("index") },
				generateEvent:    func(context.Context, *activatedIdentity) error { return step("event") },
				clearUnvalidated: func(context.Context, gocql.UUID) error { return step("clear") },
				activateTeam:     func(context.Context, gocql.UUID) error { return step("team") },
			}
			got, err := api.run(context.Background(), "synthetic-key", "synthetic-code", scenario.dryrun, scenario.verify, nil)
			if !reflect.DeepEqual(calls, scenario.want) {
				t.Fatalf("calls %v; want %v", calls, scenario.want)
			}
			if scenario.fail != "" {
				if !errors.Is(err, failure) || got != nil {
					t.Fatal("failed step did not stop activation")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if scenario.repeat || scenario.dryrun {
				if got != nil {
					t.Fatal("empty result expected")
				}
			} else if scenario.fail == "" && got != result {
				t.Fatal("activated identity lost")
			}
		})
	}
}
func TestActivationAPIRequiresSideEffectsBeforeMutation(t *testing.T) {
	called := false
	api := activationAPI{activate: func(context.Context, string, string, *gocql.UUID) (*activatedIdentity, error) {
		called = true
		return nil, nil
	}}
	if _, err := api.run(context.Background(), "key", "code", false, false, nil); err == nil || called {
		t.Fatal("activation allowed without required downstream dependencies")
	}
}
