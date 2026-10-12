// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"strings"
	"testing"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func TestProviderRegistrationAdmission(t *testing.T) {
	base := map[string]any{"name": "Synthetic", "email": "synthetic@example.com", "url": "https://example.com/provider", "description": "Synthetic description"}
	for _, password := range []any{nil, "Goodp6"} {
		base["password"] = password
		raw, _ := json.Marshal(base)
		if _, err := decodeProviderRegistration(raw); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		field string
		value any
	}{{"name", ""}, {"name", strings.Repeat("界", 129)}, {"email", "not-an-email"}, {"url", "http://example.com"}, {"description", ""}, {"description", strings.Repeat("界", 1025)}, {"password", "short"}, {"password", false}} {
		copy := map[string]any{}
		for k, v := range base {
			copy[k] = v
		}
		copy[tc.field] = tc.value
		raw, _ := json.Marshal(copy)
		if _, err := decodeProviderRegistration(raw); err == nil {
			t.Fatal("invalid provider registration accepted", tc.field)
		}
	}
	generated, err := newProviderPassword()
	decoded, decodeErr := base64.StdEncoding.DecodeString(generated)
	if err != nil || decodeErr != nil || len(generated) != 16 || len(decoded) != 12 {
		t.Fatal("native generated password shape changed")
	}
}

func TestProviderRegistrationWriteOrdering(t *testing.T) {
	id, _ := gocql.ParseUUID("11111111-2222-3333-4444-555555555555")
	for _, tc := range []struct {
		name             string
		generated, taken bool
		fail             string
		steps            []string
	}{
		{"supplied", false, false, "", []string{"key", "hash", "create", "code-generate", "throttle", "throttle-write", "code-write", "mail"}},
		{"generated", true, false, "", []string{"key", "password-generate", "hash", "create", "code-generate", "throttle", "throttle-write", "code-write", "mail"}},
		{"taken", false, true, "", []string{"key"}},
		{"throttled after account write", false, false, "throttle", []string{"key", "hash", "create", "code-generate", "throttle"}},
		{"mail failure retains account/code", false, false, "mail", []string{"key", "hash", "create", "code-generate", "throttle", "throttle-write", "code-write", "mail"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var steps []string
			step := func(name string) error {
				steps = append(steps, name)
				if tc.fail == name {
					return context.Canceled
				}
				return nil
			}
			ip := netip.MustParseAddr("127.0.0.51")
			s := providerRegistrationService{
				auth: providerAuthHTTP{byEmail: func(context.Context, string) (*gocql.UUID, error) {
					step("key")
					if tc.taken {
						return &id, nil
					}
					return nil, nil
				}, hash: func(_ context.Context, key passwordRateKey, plain string, minimum int) (string, error) {
					want := 6
					if tc.generated {
						want = 8
					}
					if key.kind != "ip" || key.ip != ip || minimum != want {
						t.Fatal("wrong native IP hash policy")
					}
					return "synthetic hash", step("hash")
				}},
				create: func(_ context.Context, r providerRegistrationRequest, encoded string) (gocql.UUID, error) {
					if encoded != "synthetic hash" {
						t.Fatal("wrong hash write")
					}
					return id, step("create")
				},
				newPassword: func() (string, error) { return "SyntheticGen1234", step("password-generate") },
				codes: providerResetService{newValue: func() (string, error) { return "ABCDEFGHIJKLMNOPQRST", step("code-generate") }, throttle: func(_ context.Context, key string, scope int32) (*int32, error) {
					if scope != 2 {
						t.Fatal("wrong activation scope")
					}
					return nil, step("throttle")
				}, insertThrottle: func(_ context.Context, key string, scope, ttl int32) error {
					if scope != 2 || ttl != 5 {
						t.Fatal("wrong throttle")
					}
					return step("throttle-write")
				}, insert: func(_ context.Context, scope int32, c providerVerificationCode) error {
					if scope != 2 || c.account == nil || *c.account != id || c.retries != 3 || c.ttl != 86400 {
						t.Fatal("wrong activation code")
					}
					return step("code-write")
				}, throttleTTL: 5},
				mail: func(_ context.Context, name, email, key, value string, update bool) error {
					if name != "Synthetic" || email != "synthetic@example.com" || update {
						t.Fatal("wrong activation mail")
					}
					return step("mail")
				},
			}
			password := "Goodp6"
			request := providerRegistrationRequest{name: "Synthetic", email: "synthetic@example.com", password: &password}
			if tc.generated {
				request.password = nil
			}
			reply, err := s.register(context.Background(), ip, request)
			if !reflect.DeepEqual(steps, tc.steps) {
				t.Fatal("changed native order", steps)
			}
			if tc.taken {
				if !errors.Is(err, errProviderEmailExists) {
					t.Fatal(err)
				}
			} else if tc.fail != "" {
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			} else if err != nil || reply.ID != id || (reply.Password != nil) != tc.generated {
				t.Fatal("wrong registration result", err)
			}
		})
	}
}

func TestProviderActivationOrdering(t *testing.T) {
	id, _ := gocql.ParseUUID("11111111-2222-3333-4444-555555555555")
	other, _ := gocql.ParseUUID("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	for _, tc := range []struct {
		name      string
		old       *string
		collision bool
		fail      string
		steps     []string
	}{
		{"initial", nil, false, "", []string{"code", "data", "owner", "activate", "approval"}},
		{"replay", providerRegistrationTextPtr("new@example.com"), false, "", []string{"code", "data"}},
		{"changed", providerRegistrationTextPtr("old@example.com"), false, "", []string{"code", "data", "remove-reset", "owner", "activate"}},
		{"changed collision removes old reset first", providerRegistrationTextPtr("old@example.com"), true, "", []string{"code", "data", "remove-reset", "owner"}},
		{"approval failure retains activated state", nil, false, "approval", []string{"code", "data", "owner", "activate", "approval"}},
		{"batch failure", providerRegistrationTextPtr("old@example.com"), false, "activate", []string{"code", "data", "remove-reset", "owner", "activate"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var steps []string
			step := func(name string) error {
				steps = append(steps, name)
				if name == tc.fail {
					return context.Canceled
				}
				return nil
			}
			s := providerRegistrationService{codes: providerResetService{
				lookup: func(_ context.Context, key string, scope int32) (*providerVerificationCode, error) {
					if scope != 2 {
						t.Fatal("wrong scope")
					}
					step("code")
					return &providerVerificationCode{email: "new@example.com", value: "NativeCode", retries: 3, account: &id}, nil
				},
				insert: func(context.Context, int32, providerVerificationCode) error {
					t.Fatal("successful activation rewrote code")
					return nil
				},
				remove: func(_ context.Context, key string, scope int32) error {
					expected, _ := emailVerificationKey("old@example.com")
					if key != expected || scope != 3 {
						t.Fatal("wrong reset removed")
					}
					return step("remove-reset")
				},
			}, data: func(context.Context, gocql.UUID) (*providerAccountData, error) {
				step("data")
				return &providerAccountData{name: "Synthetic", email: tc.old}, nil
			},
				auth: providerAuthHTTP{byEmail: func(context.Context, string) (*gocql.UUID, error) {
					step("owner")
					if tc.collision {
						return &other, nil
					}
					return nil, nil
				}},
				activateKey: func(_ context.Context, actual gocql.UUID, old *string, email string) error {
					if actual != id || old != tc.old || email != "new@example.com" {
						t.Fatal("wrong key write")
					}
					return step("activate")
				},
				approval: func(_ context.Context, name, email string) error {
					if name != "Synthetic" || email != "new@example.com" {
						t.Fatal("wrong approval")
					}
					return step("approval")
				},
			}
			email, err := s.activate(context.Background(), "ABCDEFGHIJKLMNOPQRST", "NativeCode")
			if !reflect.DeepEqual(steps, tc.steps) {
				t.Fatal("changed native activation order", steps)
			}
			if tc.collision {
				if !errors.Is(err, errProviderEmailExists) {
					t.Fatal(err)
				}
			} else if tc.fail != "" {
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			} else if err != nil || (email == nil) != (tc.name == "replay") {
				t.Fatal("wrong activation result", err)
			}
		})
	}
}

func TestProviderActivationAttemptsAndMail(t *testing.T) {
	row := providerVerificationCode{key: "ABCDEFGHIJKLMNOPQRST", value: "NativeCode", retries: 3, ttl: 731}
	s := providerRegistrationService{codes: providerResetService{lookup: func(context.Context, string, int32) (*providerVerificationCode, error) {
		copy := row
		return &copy, nil
	}, insert: func(_ context.Context, scope int32, c providerVerificationCode) error {
		if scope != 2 || c.ttl != 731 || c.value != row.value {
			t.Fatal("activation retry changed TTL/value")
		}
		row = c
		return nil
	}}, data: func(context.Context, gocql.UUID) (*providerAccountData, error) {
		t.Fatal("invalid activation reached account")
		return nil, nil
	}}
	for i := 0; i < 3; i++ {
		if _, err := s.activate(context.Background(), row.key, "WrongCode"); !errors.Is(err, errProviderVerificationCode) {
			t.Fatal(err)
		}
	}
	if row.retries != 0 {
		t.Fatal("activation retries not exhausted")
	}
	if _, err := s.activate(context.Background(), row.key, row.value); !errors.Is(err, errProviderVerificationCode) {
		t.Fatal("exhausted activation accepted")
	}
	w := httptest.NewRecorder()
	providerRegistrationHTTPError(w, deletionCodeThrottled{5})
	if w.Code != 429 || w.Header().Get("Retry-After") != "5" || w.Result().Header.Get("Content-Type") != "" {
		t.Fatal("native throttle media changed")
	}
	c, err := LoadConfig("../../../services/brig/brig.integration.yaml")
	if err != nil {
		t.Fatal(err)
	}
	c.Other["emailSMS"].(map[string]any)["general"].(map[string]any)["templateDir"] = "../../../libs/wire-subsystems/templates"
	r, err := newRegistrationMailRenderer(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, update := range []bool{false, true} {
		m, err := r.providerActivation("Synthetic <&>", "synthetic@example.com", row.key, row.value, update)
		if err != nil || m.headers[0] != [2]string{"X-Zeta-Purpose", "ProviderActivation"} || !strings.Contains(m.html, "key=ABCDEFGHIJKLMNOPQRST&amp;code=NativeCode") || m.toName == nil {
			t.Fatal("native activation/update mail changed", err)
		}
	}
	m, err := r.providerApproval("Synthetic <&>", "synthetic@example.com")
	if err != nil || m.headers[0] != [2]string{"X-Zeta-Purpose", "ProviderApprovalConfirm"} || m.toName == nil || *m.toName != "Synthetic <&>" {
		t.Fatal("native approval mail changed", err)
	}
}

func providerRegistrationTextPtr(value string) *string { return &value }
