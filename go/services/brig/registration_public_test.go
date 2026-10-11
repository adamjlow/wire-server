// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"
	"github.com/apache/cassandra-gocql-driver/v2"
	"net/netip"
	"reflect"
	"testing"
	"time"
)

func TestPublicRegistrationNativePolicyBeforeCreation(t *testing.T) {
	email, upper, code := "synthetic@example.com", "synthetic@EXAMPLE.COM", "synthetic-code"
	allowed, empty := []string{"example.com"}, []string{}
	for _, c := range []struct {
		name    string
		request registrationRequest
		policy  registrationPolicy
		want    error
	}{
		{"ephemeral", registrationRequest{}, registrationPolicy{restrict: true, ephemeral: true}, nil},
		{"restricted-personal", registrationRequest{User: newRegistrationUser{Email: &email}}, registrationPolicy{restrict: true, ephemeral: true}, errRegistrationRestricted},
		{"restricted-creator", registrationRequest{User: newRegistrationUser{Email: &email}, Origin: registrationOrigin{Team: &registrationTeam{}}}, registrationPolicy{restrict: true, ephemeral: true}, errRegistrationRestricted},
		{"allowed-member", registrationRequest{User: newRegistrationUser{Email: &email}, Origin: registrationOrigin{TeamCode: &code}}, registrationPolicy{restrict: true, ephemeral: false}, nil},
		{"disabled-ephemeral", registrationRequest{}, registrationPolicy{}, errRegistrationEphemeralDisabled},
		{"allowlisted", registrationRequest{User: newRegistrationUser{Email: &email}}, registrationPolicy{allowDomains: &allowed}, nil},
		{"empty-allowlist", registrationRequest{User: newRegistrationUser{Email: &email}}, registrationPolicy{allowDomains: &empty}, errRegistrationNotAllowlisted},
		{"exact-domain-case", registrationRequest{User: newRegistrationUser{Email: &upper}}, registrationPolicy{allowDomains: &allowed}, errRegistrationNotAllowlisted},
	} {
		t.Run(c.name, func(t *testing.T) {
			if err := c.policy.check(c.request); !errors.Is(err, c.want) {
				t.Fatal("native registration policy", err)
			}
			if c.want != nil {
				called := false
				s := publicRegistration{policy: c.policy, create: func(context.Context, registrationRequest, passwordRateKey) (registrationCreation, error) {
					called = true
					return registrationCreation{}, nil
				}}
				if _, err := s.register(context.Background(), c.request, netip.MustParseAddr("127.0.0.42")); !errors.Is(err, c.want) || called {
					t.Fatal("policy error reached creation", err)
				}
			}
		})
	}
	c, err := LoadConfig("../../../services/brig/brig.integration.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if policy, err := newRegistrationPolicy(c); err != nil || !policy.ephemeral || policy.restrict || policy.allowDomains != nil {
		t.Fatal("native configuration policy", err)
	}
	c.Settings.EphemeralCreation = nil
	if _, err := newRegistrationPolicy(c); err == nil {
		t.Fatal("missing required native ephemeral option silently defaulted")
	}
}

func TestPublicRegistrationMailAndCookieOrder(t *testing.T) {
	for _, c := range []struct {
		name                                        string
		email, activation, team, creator, ephemeral bool
		fail                                        string
		want                                        []string
	}{
		{name: "ephemeral", ephemeral: true, want: []string{"create", "cookie"}},
		{name: "activation", email: true, activation: true, want: []string{"create", "activation", "cookie"}},
		{name: "creator-activation", email: true, activation: true, team: true, creator: true, want: []string{"create", "activation", "cookie"}},
		{name: "verified-creator", email: true, team: true, creator: true, want: []string{"create", "cookie"}},
		{name: "invited-member", email: true, team: true, want: []string{"create", "welcome", "cookie"}},
		{name: "creation-failure", email: true, activation: true, fail: "create", want: []string{"create"}},
		{name: "activation-failure", email: true, activation: true, fail: "activation", want: []string{"create", "activation"}},
		{name: "welcome-failure", email: true, team: true, fail: "welcome", want: []string{"create", "welcome"}},
		{name: "cookie-failure", fail: "cookie", want: []string{"create", "cookie"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			calls := []string{}
			failure := errors.New("synthetic failure")
			now := time.Unix(1700000000, 0)
			user, team := gocql.UUID{1}, gocql.UUID{2}
			email, label, code := "synthetic@example.com", "synthetic-label", "synthetic-code"
			step := func(name string) error {
				calls = append(calls, name)
				if name == c.fail {
					return failure
				}
				return nil
			}
			request := registrationRequest{User: newRegistrationUser{Name: "Synthetic"}, Label: &label}
			profile := selfProfile{ID: user.String(), Status: "active", Name: "Synthetic"}
			if c.email {
				request.User.Email = &email
				profile.Email = &email
			}
			if c.ephemeral {
				profile.Status = "ephemeral"
			}
			if c.team {
				if c.creator {
					request.Origin.Team = &registrationTeam{Name: "Team"}
				} else {
					request.Origin.TeamCode = &code
				}
			}
			s := publicRegistration{policy: registrationPolicy{ephemeral: true}, now: func() time.Time { return now },
				create: func(_ context.Context, actual registrationRequest, key passwordRateKey) (registrationCreation, error) {
					if key.kind != "ip" || key.ip != netip.MustParseAddr("127.0.0.42") || actual.Label == nil || *actual.Label != label {
						t.Fatal("registration rate/label changed")
					}
					result := registrationCreation{profile: profile}
					if c.activation {
						result.activation = &activationRecord{key: "key", code: "code"}
					}
					if c.team {
						result.team = &registrationCreatedTeam{ID: team, Name: "Native team"}
					}
					return result, step("create")
				},
				activationMail: func(_ context.Context, actual registrationCreation, origin registrationOrigin) error {
					if actual.activation == nil || (origin.Team != nil) != c.creator {
						t.Fatal("wrong activation mail selection")
					}
					return step("activation")
				},
				welcomeMail: func(_ context.Context, actual selfProfile, created registrationCreatedTeam) error {
					if actual.Email == nil || created.ID != team {
						t.Fatal("wrong welcome mail")
					}
					return step("welcome")
				},
				cookie: func(_ context.Context, id gocql.UUID, typ int32, actualLabel *string, at time.Time) (issuedCookie, error) {
					if id != user || (typ == 0) != c.ephemeral || actualLabel == nil || *actualLabel != label || !at.Equal(now) {
						t.Fatal("wrong registration cookie parameters")
					}
					return issuedCookie{}, step("cookie")
				},
			}
			result, err := s.register(context.Background(), request, netip.MustParseAddr("127.0.0.42"))
			if !reflect.DeepEqual(calls, c.want) || c.fail != "" && !errors.Is(err, failure) || c.fail == "" && (err != nil || result.profile.ID != user.String()) {
				t.Fatal("native mail/cookie order changed", calls, err)
			}
		})
	}
}

func TestPublicRegistrationUsesUnrestrictedNativeCookieAndLabelRevocation(t *testing.T) {
	manager, store, user := testSessionManager()
	now := time.Unix(1700000000, 0)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := manager.newCookie(ctx, user, nil, 1, nil, false, false, now.Add(-time.Duration(i+1)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	create := func(context.Context, registrationRequest, passwordRateKey) (registrationCreation, error) {
		return registrationCreation{profile: selfProfile{ID: user.String(), Status: "active"}}, nil
	}
	noActivation := func(context.Context, registrationCreation, registrationOrigin) error {
		t.Fatal("unexpected activation mail")
		return nil
	}
	noWelcome := func(context.Context, selfProfile, registrationCreatedTeam) error {
		t.Fatal("unexpected welcome mail")
		return nil
	}
	registrations := newPublicRegistration(registrationPolicy{ephemeral: true}, create, noActivation, noWelcome, manager, func() time.Time { return now })
	result, err := registrations.register(ctx, registrationRequest{}, netip.MustParseAddr("127.0.0.42"))
	if err != nil || len(store.cookies) != 4 || result.cookie.token.Kind != "u" || result.cookie.token.Client != nil || result.cookie.record.Type != 1 {
		t.Fatal("registration used login limit/access/client cookie", err)
	}
	label := "synthetic-label"
	if _, err := manager.newCookie(ctx, user, nil, 0, &label, false, false, now); err != nil {
		t.Fatal(err)
	}
	refreshed := 0
	manager.refreshSuggested = func(context.Context, gocql.UUID) error { refreshed++; return nil }
	registrations = newPublicRegistration(registrationPolicy{ephemeral: true}, func(context.Context, registrationRequest, passwordRateKey) (registrationCreation, error) {
		return registrationCreation{profile: selfProfile{ID: user.String(), Status: "ephemeral"}}, nil
	}, noActivation, noWelcome, manager, func() time.Time { return now })
	result, err = registrations.register(ctx, registrationRequest{Label: &label}, netip.MustParseAddr("127.0.0.42"))
	if err != nil || len(store.cookies) != 5 || refreshed != 1 || result.cookie.record.Type != 0 || result.cookie.token.Kind != "u" {
		t.Fatal("session registration same-label revocation", err)
	}
}
