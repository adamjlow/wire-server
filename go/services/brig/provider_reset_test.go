// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func TestProviderResetBeginOrdering(t *testing.T) {
	id, _ := gocql.ParseUUID("11111111-2222-3333-4444-555555555555")
	for _, tc := range []struct {
		name                       string
		pending, throttle, missing bool
		fail                       string
		steps                      []string
	}{
		{"send", false, false, false, "", []string{"identity", "lookup", "generate", "throttle", "throttle-write", "code-write", "mail"}},
		{"pending exhausted code", true, true, false, "", []string{"identity", "lookup"}},
		{"throttled", false, true, false, "", []string{"identity", "lookup", "generate", "throttle"}},
		{"missing identity", false, false, true, "", []string{"identity"}},
		{"mail failure retains code", false, false, false, "mail", []string{"identity", "lookup", "generate", "throttle", "throttle-write", "code-write", "mail"}},
		{"code failure retains throttle", false, false, false, "code-write", []string{"identity", "lookup", "generate", "throttle", "throttle-write", "code-write"}},
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
			key, _ := emailVerificationKey("synthetic@example.com")
			check := func(k string, scope int32) {
				if k != key || scope != 3 {
					t.Fatal("wrong native code key/scope")
				}
			}
			s := providerResetService{
				auth: providerAuthHTTP{byEmail: func(context.Context, string) (*gocql.UUID, error) {
					step("identity")
					if tc.missing {
						return nil, nil
					}
					return &id, nil
				}},
				lookup: func(_ context.Context, k string, scope int32) (*providerVerificationCode, error) {
					check(k, scope)
					step("lookup")
					if tc.pending {
						return &providerVerificationCode{retries: 0, ttl: 123}, nil
					}
					return nil, nil
				},
				newValue: func() (string, error) { return "ABCDEFGHIJKLMNOPQRST", step("generate") },
				throttle: func(_ context.Context, k string, scope int32) (*int32, error) {
					check(k, scope)
					step("throttle")
					if tc.throttle {
						v := int32(4)
						return &v, nil
					}
					return nil, nil
				},
				insertThrottle: func(_ context.Context, k string, scope, ttl int32) error {
					check(k, scope)
					if ttl != 5 {
						t.Fatal("wrong throttle TTL")
					}
					return step("throttle-write")
				},
				insert: func(_ context.Context, scope int32, c providerVerificationCode) error {
					check(c.key, scope)
					if c.value != "ABCDEFGHIJKLMNOPQRST" || c.email != "synthetic@example.com" || c.account == nil || *c.account != id || c.retries != 3 || c.ttl != 3600 {
						t.Fatal("wrong reset code")
					}
					return step("code-write")
				},
				mail: func(_ context.Context, email, k, value string) error {
					if email != "synthetic@example.com" || k != key || value != "ABCDEFGHIJKLMNOPQRST" {
						t.Fatal("wrong mail")
					}
					return step("mail")
				},
				remove: func(context.Context, string, int32) error { t.Fatal("unexpected code cleanup"); return nil }, throttleTTL: 5,
			}
			err := s.begin(context.Background(), "synthetic@example.com")
			if !reflect.DeepEqual(steps, tc.steps) {
				t.Fatal("changed native ordering", steps)
			}
			var pending providerResetPending
			var throttle deletionCodeThrottled
			if tc.pending {
				if !errors.As(err, &pending) || pending.ttl != 123 {
					t.Fatal("pending code lost", err)
				}
			} else if tc.throttle {
				if !errors.As(err, &throttle) {
					t.Fatal("throttle lost", err)
				}
			} else if tc.missing {
				if !errors.Is(err, authInvalidCredentials) {
					t.Fatal("unknown provider accepted")
				}
			} else if tc.fail != "" {
				if !errors.Is(err, context.Canceled) {
					t.Fatal("failure swallowed")
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestProviderCodeAttemptsPreserveTTL(t *testing.T) {
	row := providerVerificationCode{key: "ABCDEFGHIJKLMNOPQRST", value: "NativeSecretCode1234", email: "synthetic@example.com", ttl: 137, retries: 3}
	writes := 0
	s := providerResetService{lookup: func(context.Context, string, int32) (*providerVerificationCode, error) {
		copy := row
		return &copy, nil
	}, insert: func(_ context.Context, scope int32, c providerVerificationCode) error {
		writes++
		if scope != 3 || c.ttl != 137 || c.key != row.key || c.value != row.value || c.email != row.email {
			t.Fatal("failed attempt changed native fields/TTL")
		}
		row = c
		return nil
	}}
	for i := 0; i < 3; i++ {
		code, err := s.verify(context.Background(), row.key, "WrongCode")
		if err != nil || code != nil || row.retries != uint8(2-i) {
			t.Fatal("wrong retry accounting", err)
		}
	}
	if code, err := s.verify(context.Background(), row.key, row.value); err != nil || code != nil || writes != 3 {
		t.Fatal("exhausted code accepted")
	}
	row.retries = 1
	if code, err := s.verify(context.Background(), row.key, row.value); err != nil || code == nil || writes != 3 {
		t.Fatal("valid verification consumed code")
	}
}

func TestProviderResetMutationOrder(t *testing.T) {
	id, _ := gocql.ParseUUID("11111111-2222-3333-4444-555555555555")
	for _, tc := range []struct {
		name          string
		same, missing bool
		fail          string
		want          error
		steps         []string
	}{
		{"reset", false, false, "", nil, []string{"lookup", "password", "verify", "hash", "write", "delete"}},
		{"same", true, false, "", errPasswordResetSame, []string{"lookup", "password", "verify"}},
		{"unbound code", false, true, "", errPasswordResetCode, []string{"lookup"}},
		{"write failure", false, false, "write", context.Canceled, []string{"lookup", "password", "verify", "hash", "write"}},
		{"delete failure", false, false, "delete", context.Canceled, []string{"lookup", "password", "verify", "hash", "write", "delete"}},
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
			s := providerResetService{
				lookup: func(context.Context, string, int32) (*providerVerificationCode, error) {
					step("lookup")
					account := &id
					if tc.missing {
						account = nil
					}
					return &providerVerificationCode{value: "NativeCode", retries: 3, account: account}, nil
				},
				insert: func(context.Context, int32, providerVerificationCode) error {
					t.Fatal("valid code was rewritten")
					return nil
				},
				remove: func(_ context.Context, key string, scope int32) error {
					if key != "ABCDEFGHIJKLMNOPQRST" || scope != 3 {
						t.Fatal("wrong code deletion")
					}
					return step("delete")
				},
				auth: providerAuthHTTP{
					password: func(context.Context, gocql.UUID) (*string, error) {
						step("password")
						v := "synthetic hash"
						return &v, nil
					},
					verify: func(_ context.Context, key passwordRateKey, encoded, plain string) (bool, bool, error) {
						if key.kind != "provider" || key.id != id.String() || plain != "Newp6!" {
							t.Fatal("wrong provider verification")
						}
						return tc.same, true, step("verify")
					},
					hash: func(_ context.Context, key passwordRateKey, plain string, minimum int) (string, error) {
						if key.kind != "provider" || minimum != 6 {
							t.Fatal("wrong hashing policy")
						}
						return "new synthetic hash", step("hash")
					},
					write: func(_ context.Context, actual gocql.UUID, value string) error {
						if actual != id || value != "new synthetic hash" {
							t.Fatal("wrong password write")
						}
						return step("write")
					},
				},
			}
			err := s.complete(context.Background(), providerResetRequest{key: "ABCDEFGHIJKLMNOPQRST", code: "NativeCode", password: "Newp6!"})
			if !errors.Is(err, tc.want) || !reflect.DeepEqual(steps, tc.steps) {
				t.Fatal("changed reset outcome/order", err, steps)
			}
		})
	}
}

func TestProviderResetAdmissionAndGuards(t *testing.T) {
	for _, body := range []string{`{"key":"ABCDEFGHIJKLMNOPQRST","code":"123456","password":"Newp6!"}`, `{"key":"ABCDEFGHIJKLMNOPQRST","code":"ABCDEFGHIJKLMNOPQRST","password":"` + strings.Repeat("界", 1024) + `"}`} {
		if _, err := decodeProviderReset(json.RawMessage(body), true); err != nil {
			t.Fatal("valid native reset rejected", err)
		}
	}
	for _, body := range []string{`null`, `{}`, `{"email":null}`, `{"key":"short","code":"123456","password":"Newp6!"}`, `{"key":"ABCDEFGHIJKLMNOPQRST","code":"short","password":"Newp6!"}`, `{"key":"ABCDEFGHIJKLMNOPQRST","code":"123456","password":"short"}`} {
		if _, err := decodeProviderReset(json.RawMessage(body), true); err == nil {
			t.Fatal("invalid reset accepted")
		}
	}
	lookups := 0
	s := providerResetService{auth: providerAuthHTTP{secondFactorEnabled: func(context.Context, string) (bool, error) { return true, nil }}, lookup: func(context.Context, string, int32) (*providerVerificationCode, error) {
		lookups++
		return &providerVerificationCode{value: "NativeCode", retries: 0}, nil
	}}
	r := httptest.NewRequest(http.MethodPost, "/provider/password-reset", strings.NewReader(`{"email":"synthetic@example.com"}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handle(w, r, r.URL.Path)
	if w.Code != 403 || lookups != 0 {
		t.Fatal("public feature guard bypass")
	}
	r = httptest.NewRequest(http.MethodGet, "/i/provider/password-reset-code?email=synthetic@example.com", nil)
	w = httptest.NewRecorder()
	s.handle(w, r, r.URL.Path)
	if w.Code != 200 || lookups != 1 || !strings.Contains(w.Body.String(), "NativeCode") {
		t.Fatal("internal exhausted code lookup changed")
	}
}

func TestProviderResetNativeMail(t *testing.T) {
	c, err := LoadConfig("../../../services/brig/brig.integration.yaml")
	if err != nil {
		t.Fatal(err)
	}
	c.Other["emailSMS"].(map[string]any)["general"].(map[string]any)["templateDir"] = "../../../libs/wire-subsystems/templates"
	c.Settings.DefaultLocale = "de"
	r, err := newRegistrationMailRenderer(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	message, err := r.providerPasswordReset("synthetic@example.com", "ABCDEFGHIJKLMNOPQRST", "NativeCode")
	if err != nil {
		t.Fatal(err)
	}
	if message.subject != "Password Change: Wire Service Provider" || message.toName != nil || message.to != "synthetic@example.com" || !strings.Contains(message.plain, "key=ABCDEFGHIJKLMNOPQRST&code=NativeCode") || !strings.Contains(message.html, "key=ABCDEFGHIJKLMNOPQRST&amp;code=NativeCode") || message.headers[0] != [2]string{"X-Zeta-Purpose", "ProviderPasswordReset"} {
		t.Fatal("native provider reset mail changed")
	}
}

func TestProviderResetPendingNativeMedia(t *testing.T) {
	w := httptest.NewRecorder()
	providerResetHTTPError(w, providerResetPending{123})
	if w.Code != 409 || w.Result().Header.Get("Content-Type") != "" || w.Header().Get("Retry-After") != "123" || !strings.Contains(w.Body.String(), `"code-exists"`) {
		t.Fatal("native pending reset headers/body changed")
	}
}
