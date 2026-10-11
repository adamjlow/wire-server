// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/apache/cassandra-gocql-driver/v2"
	"math/big"
	"reflect"
	"testing"
	"time"
)

func TestRegistrationInvitationGuardsRunBeforeAccountMutation(t *testing.T) {
	for _, scenario := range []struct {
		name    string
		email   *string
		missing bool
		invite  int32
		other   bool
		total   int64
		fail    string
		want    error
		steps   []string
	}{
		{name: "missing-identity", want: errRegistrationMissingIdentity, steps: []string{}},
		{name: "missing-code", email: invitationTestString("synthetic@example.com"), missing: true, want: errRegistrationInvalidInvitation, steps: []string{"lookup"}},
		{name: "wrong-email", email: invitationTestString("other@example.com"), want: errRegistrationInvalidInvitation, steps: []string{"lookup"}},
		{name: "not-allowed", email: invitationTestString("synthetic@example.com"), invite: 2, want: errInviteNotAllowed, steps: []string{"lookup", "domain"}},
		{name: "other-team", email: invitationTestString("synthetic@example.com"), invite: 3, other: true, want: errInviteOtherTeam, steps: []string{"lookup", "domain"}},
		{name: "at-limit", email: invitationTestString("synthetic@example.com"), total: 32, want: errRegistrationTooManyTeamMembers, steps: []string{"lookup", "domain", "size"}},
		{name: "allowed-team", email: invitationTestString("Synthetic+alias@EXAMPLE.COM"), invite: 3, total: 31, steps: []string{"lookup", "domain", "size", "check"}},
		{name: "lookup-failure", email: invitationTestString("synthetic@example.com"), fail: "lookup", steps: []string{"lookup"}},
		{name: "domain-failure", email: invitationTestString("synthetic@example.com"), fail: "domain", steps: []string{"lookup", "domain"}},
		{name: "size-failure", email: invitationTestString("synthetic@example.com"), fail: "size", steps: []string{"lookup", "domain", "size"}},
		{name: "admission-failure", email: invitationTestString("synthetic@example.com"), fail: "check", steps: []string{"lookup", "domain", "size", "check"}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			calls := []string{}
			failure := errors.New("synthetic failure")
			step := func(name string) error {
				calls = append(calls, name)
				if scenario.fail == name {
					return failure
				}
				return nil
			}
			inv := storedInvitation{Team: gocql.UUID{1}, ID: gocql.UUID{2}, Email: "synthetic@example.com"}
			resolver := registrationInvitations{maxSize: 32,
				lookup: func(context.Context, string) (*storedInvitation, error) {
					err := step("lookup")
					if scenario.missing {
						return nil, err
					}
					return &inv, err
				},
				domain: func(_ context.Context, domain string) (*domainRegistration, error) {
					if domain != "example.com" {
						t.Fatal("domain not canonical")
					}
					team := inv.Team
					if scenario.other {
						team = gocql.UUID{3}
					}
					return &domainRegistration{invite: &scenario.invite, team: &team}, step("domain")
				},
				size: func(context.Context, gocql.UUID) (teamSize, error) {
					return teamSize{Total: big.NewInt(scenario.total)}, step("size")
				},
				checkJoin: func(context.Context, gocql.UUID) error { return step("check") },
			}
			got, err := resolver.find(context.Background(), scenario.email, "synthetic-code")
			expected := scenario.want
			if scenario.fail != "" {
				expected = failure
			}
			if !errors.Is(err, expected) || !reflect.DeepEqual(calls, scenario.steps) || err == nil && got != &inv {
				t.Fatal("invitation guard order/result", calls, err)
			}
		})
	}
}

func TestRegistrationInvitationActivationOrderingAndSCIMIdentity(t *testing.T) {
	for _, scenario := range []struct {
		name, fail           string
		claimed, added, scim bool
	}{
		{name: "ordinary", claimed: true, added: true}, {name: "scim", claimed: true, added: true, scim: true},
		{name: "claim-denied", added: true}, {name: "member-denied", claimed: true},
		{name: "scim-name", claimed: true, added: true, scim: true, fail: "scim-name"},
		{name: "claim-error", claimed: true, added: true, fail: "claim"},
		{name: "member-error", claimed: true, added: true, fail: "join"},
		{name: "activation-error", claimed: true, added: true, fail: "activate"},
		{name: "event-error", claimed: true, added: true, fail: "event"},
		{name: "pending-error", claimed: true, added: true, fail: "pending"},
		{name: "delete-error", claimed: true, added: true, fail: "delete"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			calls := []string{}
			failure := errors.New("synthetic private dependency failure")
			step := func(name string) error {
				calls = append(calls, name)
				if scenario.fail == name {
					return failure
				}
				return nil
			}
			inv := storedInvitation{ID: gocql.UUID{2}, Team: gocql.UUID{3}, Email: "synthetic@example.com"}
			otherID := gocql.UUID{4}
			email, password, code := "Synthetic+alias@example.com", "12345678", "synthetic-code"
			request := registrationRequest{User: newRegistrationUser{Name: "Synthetic", UUID: &otherID, Email: &email}, Password: &password, Origin: registrationOrigin{TeamCode: &code}}
			handle := "synthetic-handle"
			existing := selfProfile{Name: "Synthetic", Handle: &handle, ManagedBy: "wire"}
			if scenario.scim {
				existing.ManagedBy = "scim"
				existing.SSO = json.RawMessage(`{"scim_external_id":"native-external"}`)
			}
			if scenario.fail == "scim-name" {
				existing.Name = "SCIM controlled name"
			}
			service := registrationService{
				identity: registrationIdentity{owner: func(context.Context, string) (*gocql.UUID, error) { return nil, step("owner") }, account: func(context.Context, gocql.UUID) (*storedUser, error) { t.Fatal("unexpected owner"); return nil, nil }, deleteKey: func(context.Context, string, *gocql.UUID) error { t.Fatal("unexpected stale owner"); return nil }, blacklisted: func(context.Context, string) (bool, error) { return false, step("block") }},
				guardDomain: func(context.Context, string) error {
					t.Fatal("ordinary domain guard applied to invitation")
					return nil
				},
				findInvitation: func(context.Context, *string, string) (*storedInvitation, error) { return &inv, step("find") },
				invitedAccount: func(context.Context, gocql.UUID) (*selfProfile, error) { return &existing, step("existing") },
				hash: func(context.Context, passwordRateKey, string, int) (string, error) {
					return "encoded-synthetic", step("hash")
				},
				createAccount: func(_ context.Context, u storedUser) error {
					if u.ID != inv.ID || u.Team == nil || *u.Team != inv.Team || u.Activated || u.Handle != nil {
						t.Fatal("invitation constructor precedence changed")
					}
					if scenario.scim && (!reflect.DeepEqual(u.SSO, existing.SSO) || u.ManagedBy == nil || *u.ManagedBy != 1) {
						t.Fatal("SCIM identity lost")
					}
					return step("store")
				},
				createSelf: func(context.Context, gocql.UUID) error { return step("self") },
				claimInvitationKey: func(_ context.Context, email string, id gocql.UUID) (bool, error) {
					if email != inv.Email || id != inv.ID {
						t.Fatal("wrong claim")
					}
					return scenario.claimed, step("claim")
				},
				joinInvitation: func(_ context.Context, id gocql.UUID, actual storedInvitation) (bool, error) {
					if id != inv.ID || actual.ID != inv.ID {
						t.Fatal("wrong invite")
					}
					return scenario.added, step("join")
				},
				activateStored: func(_ context.Context, id gocql.UUID, email *string, activate bool) error {
					if id != inv.ID || email == nil || *email != inv.Email || !activate {
						t.Fatal("wrong invitation activation")
					}
					return step("activate")
				},
				onAccountActivated: func(_ context.Context, event *activatedIdentity) error {
					if !event.first || event.creationProfile == nil || event.creationProfile.Handle == nil || event.creationProfile.Email == nil || *event.creationProfile.Email != email {
						t.Fatal("creation event snapshot lost")
					}
					return step("event")
				},
				removePending:    func(context.Context, gocql.UUID) error { return step("pending") },
				deleteInvitation: func(context.Context, gocql.UUID, gocql.UUID) error { return step("delete") },
				teamName:         func(context.Context, gocql.UUID) (string, error) { return "Native team", step("name") },
				newCode: func(context.Context, string, float64, *gocql.UUID) (activationRecord, error) {
					t.Fatal("invitation created activation code")
					return activationRecord{}, nil
				},
				activate: func(context.Context, string, string, *gocql.UUID, *string) error {
					t.Fatal("invitation consumed code")
					return nil
				},
				initFeature: func(context.Context, gocql.UUID) error { return step("feature") }, domain: "example.com", locale: registrationLocale{Language: "en"}, sessionTTL: 30, now: time.Now,
			}
			result, err := service.create(context.Background(), request, passwordRateKey{kind: "internal"})
			expected := []string{"owner", "block", "find", "existing", "hash", "store", "self", "claim", "join", "activate", "event", "pending", "delete", "name", "feature"}
			stop := ""
			var want error
			switch {
			case scenario.fail == "scim-name":
				stop = "existing"
				want = errRegistrationSCIMName
			case scenario.fail != "":
				stop = scenario.fail
				want = failure
			case !scenario.claimed:
				stop = "claim"
				want = errActivationKeyExists
			case !scenario.added:
				stop = "join"
				want = errRegistrationTooManyTeamMembers
			}
			if stop != "" {
				for i, name := range expected {
					if name == stop {
						expected = expected[:i+1]
						break
					}
				}
			}
			if !reflect.DeepEqual(calls, expected) || !errors.Is(err, want) {
				t.Fatal("native invitation partial-failure order", calls, err)
			}
			if err == nil && (result.profile.ID != inv.ID.String() || result.profile.Handle == nil || result.team == nil || result.team.ID != inv.Team || result.activation != nil) {
				t.Fatal("invitation result contract")
			}
		})
	}
}

func invitationTestString(s string) *string { return &s }
