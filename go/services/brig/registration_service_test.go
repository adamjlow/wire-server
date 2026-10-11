// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func TestRegistrationNativeCreationOrderingAndFailures(t *testing.T) {
	for _, scenario := range []struct {
		name                     string
		email, preverified, team bool
		fail                     string
		want                     []string
	}{
		{"ephemeral", false, false, false, "", []string{"store", "self", "feature"}},
		{"legacy-invitation", false, false, false, "", []string{"store", "self", "feature"}},
		{"identified", true, false, false, "", []string{"owner", "block", "domain", "hash", "store", "self", "code", "feature", "activate"}},
		{"preverified", true, true, false, "", []string{"owner", "block", "domain", "hash", "store", "self", "activate", "feature"}},
		{"creator", true, true, true, "", []string{"owner", "block", "domain", "hash", "store", "self", "team", "activate", "feature"}},
		{"owner-error", true, false, false, "owner", []string{"owner"}},
		{"blocked", true, false, false, "block", []string{"owner", "block"}},
		{"domain", true, false, false, "domain", []string{"owner", "block", "domain"}},
		{"hash", true, false, false, "hash", []string{"owner", "block", "domain", "hash"}},
		{"self", true, false, false, "self", []string{"owner", "block", "domain", "hash", "store", "self"}},
		{"feature", true, false, false, "feature", []string{"owner", "block", "domain", "hash", "store", "self", "code", "feature"}},
		{"activation", true, false, false, "activate", []string{"owner", "block", "domain", "hash", "store", "self", "code", "feature", "activate"}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var calls []string
			failure := errors.New("synthetic dependency failure")
			step := func(name string) error {
				calls = append(calls, name)
				if scenario.fail == name {
					return failure
				}
				return nil
			}
			id := gocql.UUID{}
			email, password, code, currency := "synthetic@example.com", "12345678", "123456", "EUR"
			request := registrationRequest{User: newRegistrationUser{Name: "Synthetic", UUID: &id}}
			if scenario.name == "legacy-invitation" {
				legacy := "synthetic"
				request.Origin.InvitationCode = &legacy
			}
			if scenario.email {
				request.User.Email = &email
				request.Password = &password
			}
			if scenario.preverified {
				request.EmailCode = &code
			}
			if scenario.team {
				request.Origin.Team = &registrationTeam{Name: "Team", Icon: "default", Currency: &currency}
			}
			var persisted storedUser
			service := registrationService{
				identity: registrationIdentity{owner: func(context.Context, string) (*gocql.UUID, error) { return nil, step("owner") }, account: func(context.Context, gocql.UUID) (*storedUser, error) {
					t.Fatal("unexpected owner read")
					return nil, nil
				}, deleteKey: func(context.Context, string, *gocql.UUID) error { t.Fatal("unexpected key delete"); return nil }, blacklisted: func(context.Context, string) (bool, error) { return false, step("block") }},
				guardDomain: func(context.Context, string) error { return step("domain") },
				hash: func(_ context.Context, key passwordRateKey, plain string, minimum int) (string, error) {
					if key.kind != "internal" || plain != password || minimum != 8 {
						t.Fatal("native hash rate/validation parameters changed")
					}
					return "synthetic-encoded-password", step("hash")
				},
				createAccount: func(_ context.Context, account storedUser) error { persisted = account; return step("store") },
				createSelf: func(_ context.Context, user gocql.UUID) error {
					if user != id {
						t.Fatal("wrong self owner")
					}
					return step("self")
				},
				createTeam: func(_ context.Context, user, team gocql.UUID, input registrationTeam) error {
					if user != id || persisted.Team == nil || *persisted.Team != team || input.Name != "Team" {
						t.Fatal("wrong creator team")
					}
					return step("team")
				},
				newCode: func(_ context.Context, actual string, timeout float64, user *gocql.UUID) (activationRecord, error) {
					if actual != email || timeout != 4 || user == nil || *user != id {
						t.Fatal("wrong registration activation pair")
					}
					return activationRecord{key: "synthetic-key", code: code}, step("code")
				},
				activate: func(_ context.Context, key, actualCode string, user *gocql.UUID, actualCurrency *string) error {
					if user == nil || *user != id || actualCode != code || (actualCurrency != nil) != scenario.team {
						t.Fatal("wrong activation override/currency")
					}
					if scenario.preverified {
						expected, err := activationEmailKey(email)
						if err != nil || key != expected {
							t.Fatal("preverification key changed")
						}
					} else if key != "synthetic-key" {
						t.Fatal("wrong freshly created activation key")
					}
					return step("activate")
				},
				initFeature: func(context.Context, gocql.UUID) error { return step("feature") }, domain: "example.com", locale: registrationLocale{Language: "en"}, sessionTTL: 30, activationTimeout: 4, now: func() time.Time { return time.Unix(100, 0) },
			}
			profile, err := service.createInternal(context.Background(), request)
			if !reflect.DeepEqual(calls, scenario.want) {
				t.Fatal("native creation order changed", calls)
			}
			if scenario.fail != "" {
				if !errors.Is(err, failure) || profile.ID != "" {
					t.Fatal("failed creation returned success", err)
				}
				return
			}
			if err != nil || profile.ID != id.String() || persisted.Activated || profile.Email != request.User.Email {
				t.Fatal("native creation snapshot changed", err)
			}
			if scenario.email {
				if persisted.Password == nil || *persisted.Password == password {
					t.Fatal("plaintext stored")
				}
			} else if profile.Status != "ephemeral" || persisted.Expires == nil {
				t.Fatal("ephemeral creation changed")
			}
		})
	}
}

func TestRegistrationUnavailableOriginsDoNotPersistPartialAccounts(t *testing.T) {
	for _, body := range []string{`{"name":"Synthetic","team_code":"synthetic","password":"12345678"}`, `{"name":"Synthetic","sso_id":{"tenant":"https://issuer.example.com","subject":"synthetic"},"team_id":"00000000-0000-0000-0000-000000000001"}`} {
		request, err := decodeRegistration(strings.NewReader(body), false)
		if err != nil {
			t.Fatal(err)
		}
		stored := false
		service := registrationService{guardDomain: func(context.Context, string) error { return nil }, hash: func(context.Context, passwordRateKey, string, int) (string, error) { return "", nil }, createAccount: func(context.Context, storedUser) error { stored = true; return nil }, createSelf: func(context.Context, gocql.UUID) error { return nil }, newCode: func(context.Context, string, float64, *gocql.UUID) (activationRecord, error) {
			return activationRecord{}, nil
		}, activate: func(context.Context, string, string, *gocql.UUID, *string) error { return nil }, initFeature: func(context.Context, gocql.UUID) error { return nil }, now: time.Now}
		if _, err := service.createInternal(context.Background(), request); err == nil || stored {
			t.Fatal("unimplemented origin persisted partial account")
		}
	}
}

func TestRegistrationSAMLAndSCIMOriginOrdering(t *testing.T) {
	for _, scenario := range []struct {
		name, fail  string
		saml, email bool
		want        []string
	}{
		{"saml", "", true, false, []string{"store", "self", "join", "activate-store", "event", "team-name", "feature"}},
		{"saml-email", "", true, true, []string{"store", "self", "join", "activate-store", "event", "team-name", "code", "feature", "activate-code"}},
		{"scim", "", false, false, []string{"store", "self", "feature"}},
		{"join-denied", "denied", true, false, []string{"store", "self", "join"}},
		{"join-error", "join", true, false, []string{"store", "self", "join"}},
		{"activation-error", "activate-store", true, false, []string{"store", "self", "join", "activate-store"}},
		{"event-error", "event", true, false, []string{"store", "self", "join", "activate-store", "event"}},
		{"name-error", "team-name", true, false, []string{"store", "self", "join", "activate-store", "event", "team-name"}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			body := `{"name":"Synthetic SSO","uuid":"00000000-0000-0000-0000-000000000001","team_id":"00000000-0000-0000-0000-000000000002","supported_protocols":[],"sso_id":`
			if scenario.saml {
				body += `{"tenant":"https://issuer.example.com","subject":"synthetic"}`
			} else {
				body += `{"scim_external_id":"synthetic"}`
			}
			if scenario.email {
				body += `,"email":"synthetic@example.com"`
			}
			body += `}`
			request, err := decodeRegistration(strings.NewReader(body), false)
			if err != nil {
				t.Fatal(err)
			}
			var calls []string
			failure := errors.New("synthetic dependency error")
			step := func(name string) error {
				calls = append(calls, name)
				if name == scenario.fail {
					return failure
				}
				return nil
			}
			service := registrationService{
				identity: registrationIdentity{owner: func(context.Context, string) (*gocql.UUID, error) { return nil, nil }, account: func(context.Context, gocql.UUID) (*storedUser, error) { return nil, nil }, deleteKey: func(context.Context, string, *gocql.UUID) error { return nil }, blacklisted: func(context.Context, string) (bool, error) { return false, nil }},
				guardDomain: func(context.Context, string) error {
					t.Fatal("SSO origin used ordinary registration domain guard")
					return nil
				}, hash: func(context.Context, passwordRateKey, string, int) (string, error) {
					t.Fatal("passwordless SSO hashed a password")
					return "", nil
				},
				createAccount: func(_ context.Context, account storedUser) error {
					if account.Team == nil || *account.Team != *request.Origin.SSOTeam || account.Activated {
						t.Fatal("SSO constructor identity/team/activation changed")
					}
					return step("store")
				},
				createSelf: func(context.Context, gocql.UUID) error { return step("self") },
				joinSSO: func(_ context.Context, user, team gocql.UUID) (bool, error) {
					if user != *request.User.UUID || team != *request.Origin.SSOTeam {
						t.Fatal("wrong membership actor/team")
					}
					return scenario.fail != "denied", step("join")
				},
				activateStored: func(_ context.Context, user gocql.UUID, email *string, activate bool) error {
					if !activate || user != *request.User.UUID || !sameRegistrationString(email, request.User.Email) {
						t.Fatal("native SSO activation changed")
					}
					return step("activate-store")
				},
				onAccountActivated: func(_ context.Context, result *activatedIdentity) error {
					if !result.first || result.creationProfile == nil || len(result.creationProfile.SSO) == 0 || len(result.creationProfile.Protocols) != 0 || !result.account.Activated {
						t.Fatal("native creation event projection changed")
					}
					return step("event")
				},
				teamName: func(context.Context, gocql.UUID) (string, error) { return "Native team", step("team-name") },
				newCode: func(context.Context, string, float64, *gocql.UUID) (activationRecord, error) {
					return activationRecord{key: "synthetic", code: "123456"}, step("code")
				}, activate: func(context.Context, string, string, *gocql.UUID, *string) error { return step("activate-code") }, initFeature: func(context.Context, gocql.UUID) error { return step("feature") }, domain: "example.com", locale: registrationLocale{Language: "en"}, sessionTTL: 30, now: time.Now,
			}
			profile, err := service.createInternal(context.Background(), request)
			if !reflect.DeepEqual(calls, scenario.want) {
				t.Fatal("native SSO creation ordering changed", calls)
			}
			if scenario.fail == "denied" {
				if !errors.Is(err, errRegistrationTooManyTeamMembers) || profile.ID != "" {
					t.Fatal("membership rejection returned success")
				}
			} else if scenario.fail != "" {
				if !errors.Is(err, failure) || profile.ID != "" {
					t.Fatal("failed join step returned success", err)
				}
			} else if err != nil || profile.ID != request.User.UUID.String() || len(profile.SSO) == 0 || len(profile.Protocols) != 0 {
				t.Fatal("native SSO response changed", err)
			}
		})
	}
}
