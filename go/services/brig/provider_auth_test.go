// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func TestProviderCredentialsAdmission(t *testing.T) {
	for _, login := range []bool{false, true} {
		field, other := "new_password", `"old_password":"Oldp6!"`
		if login {
			field, other = "password", `"email":"synthetic@example.com"`
		}
		for _, length := range []int{6, 1024} {
			body := `{` + other + `,"` + field + `":"` + strings.Repeat("界", length) + `"}`
			if _, err := decodeProviderCredentials(json.RawMessage(body), login); err != nil {
				t.Fatal("native password length rejected", login, length, err)
			}
		}
		for _, value := range []string{`null`, `false`, `"short"`, `"` + strings.Repeat("x", 1025) + `"`} {
			if _, err := decodeProviderCredentials(json.RawMessage(`{`+other+`,"`+field+`":`+value+`}`), login); err == nil {
				t.Fatal("invalid password accepted", login)
			}
		}
		if _, err := decodeProviderCredentials(json.RawMessage(`{"`+field+`":"Goodp6"}`), login); err == nil {
			t.Fatal("missing required field accepted")
		}
	}
}

func TestProviderPasswordChangeOrder(t *testing.T) {
	id, _ := gocql.ParseUUID("11111111-2222-3333-4444-555555555555")
	for _, tc := range []struct {
		name               string
		old, same, missing bool
		fail               string
		want               error
		steps              []string
	}{
		{"change", true, false, false, "", nil, []string{"read", "old", "read", "new", "hash", "write"}},
		{"wrong", false, false, false, "", authInvalidCredentials, []string{"read", "old"}},
		{"same", true, true, false, "", errPasswordChangeSame, []string{"read", "old", "read", "new"}},
		{"missing", false, false, true, "", authInvalidCredentials, []string{"read"}},
		{"read failure", true, false, false, "read", context.Canceled, []string{"read"}},
		{"verify failure", true, false, false, "old", context.Canceled, []string{"read", "old"}},
		{"hash failure", true, false, false, "hash", context.Canceled, []string{"read", "old", "read", "new", "hash"}},
		{"write failure", true, false, false, "write", context.Canceled, []string{"read", "old", "read", "new", "hash", "write"}},
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
			h := providerAuthHTTP{
				password: func(context.Context, gocql.UUID) (*string, error) {
					err := step("read")
					if tc.missing {
						return nil, err
					}
					value := "synthetic encoded hash"
					return &value, err
				},
				verify: func(_ context.Context, key passwordRateKey, encoded, plain string) (bool, bool, error) {
					if key.kind != "provider" || key.id != id.String() || encoded != "synthetic encoded hash" {
						t.Fatal("wrong password namespace/projection")
					}
					if plain == "Oldp6!" {
						return tc.old, true, step("old")
					}
					return tc.same, true, step("new")
				},
				hash: func(_ context.Context, key passwordRateKey, plain string, minimum int) (string, error) {
					if key.kind != "provider" || key.id != id.String() || minimum != 6 || plain != "Newp6!" {
						t.Fatal("changed hash policy")
					}
					return "new synthetic encoded hash", step("hash")
				},
				write: func(_ context.Context, actual gocql.UUID, encoded string) error {
					if actual != id || encoded != "new synthetic encoded hash" {
						t.Fatal("wrong write")
					}
					return step("write")
				},
			}
			if err := h.change(context.Background(), id, providerCredentials{old: "Oldp6!", password: "Newp6!"}); !errors.Is(err, tc.want) {
				t.Fatal("wrong outcome", err)
			}
			if !reflect.DeepEqual(steps, tc.steps) {
				t.Fatal("changed native work order", steps)
			}
		})
	}
}

func TestProviderLoginCookieAndBoundaries(t *testing.T) {
	id, _ := gocql.ParseUUID("11111111-2222-3333-4444-555555555555")
	now := time.Unix(1700000000, 123000000)
	private := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	for _, tc := range []struct {
		name                              string
		enabled, missing, wrong, insecure bool
		peerErr                           error
		want                              int
		reads, verifies                   int
	}{
		{name: "secure", want: 200, reads: 1, verifies: 1},
		{name: "insecure", insecure: true, want: 200, reads: 1, verifies: 1},
		{name: "missing key", missing: true, want: 403},
		{name: "wrong password", wrong: true, want: 403, reads: 1, verifies: 1},
		{name: "second factor", enabled: true, want: 403},
		{name: "feature unavailable", peerErr: context.Canceled, want: 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reads, verifies, writes := 0, 0, 0
			h := providerAuthHTTP{
				secondFactorEnabled: func(context.Context, string) (bool, error) { return tc.enabled, tc.peerErr },
				byEmail: func(_ context.Context, email string) (*gocql.UUID, error) {
					if email != "synthetic@example.com" {
						t.Fatal("email lost")
					}
					if tc.missing {
						return nil, nil
					}
					return &id, nil
				},
				password: func(context.Context, gocql.UUID) (*string, error) {
					reads++
					v := "legacy synthetic hash"
					return &v, nil
				},
				verify: func(_ context.Context, key passwordRateKey, encoded, plain string) (bool, bool, error) {
					verifies++
					if key.kind != "provider" || key.id != id.String() {
						t.Fatal("rate namespace lost")
					}
					return !tc.wrong, true, nil
				},
				write:  func(context.Context, gocql.UUID, string) error { writes++; return nil },
				issuer: tokenIssuer{key: private, index: 1}, ttl: 60, now: func() time.Time { return now }, cookieInsecure: tc.insecure,
			}
			r := httptest.NewRequest(http.MethodPost, "/provider/login", strings.NewReader(`{"email":"synthetic@example.com","password":"Goodp6"}`))
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			if !h.handle(w, r, r.URL.Path) || w.Code != tc.want || reads != tc.reads || verifies != tc.verifies || writes != 0 {
				t.Fatal("incorrect authentication boundary", w.Code, reads, verifies, writes)
			}
			cookies := w.Result().Cookies()
			if tc.want != 200 {
				if len(cookies) != 0 {
					t.Fatal("cookie on failed login")
				}
				return
			}
			if len(cookies) != 1 {
				t.Fatal("missing provider cookie")
			}
			c := cookies[0]
			if c.Name != "zprovider" || c.Path != "/provider" || !c.HttpOnly || c.Secure == tc.insecure || c.Domain != "" || c.MaxAge != 0 || c.SameSite != 0 || c.Expires.Unix() != now.Unix()+60 || w.Body.Len() != 0 {
				t.Fatal("wrong provider cookie attributes")
			}
			token, err := ParseZToken(c.Value, "p")
			if err != nil || token.Provider != id.String() || token.Key != 1 || token.Verify([]ed25519.PublicKey{private.Public().(ed25519.PublicKey)}, now) != nil {
				t.Fatal("provider cookie not signed correctly", err)
			}
		})
	}
}

func TestProviderPasswordHTTPGuard(t *testing.T) {
	for _, typ := range []string{"", "user", "provider"} {
		calls := 0
		h := providerAuthHTTP{secondFactorEnabled: func(context.Context, string) (bool, error) { calls++; return true, nil }}
		r := httptest.NewRequest(http.MethodPut, "/provider/password", strings.NewReader(`{"old_password":"Oldp6!","new_password":"Newp6!"}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Z-Type", typ)
		r.Header.Set("Z-Provider", "11111111-2222-3333-4444-555555555555")
		w := httptest.NewRecorder()
		h.handle(w, r, r.URL.Path)
		if w.Code != 403 {
			t.Fatal("provider guard bypass")
		}
		if typ == "provider" {
			if calls != 1 || !strings.Contains(w.Body.String(), `"access-denied"`) {
				t.Fatal("second factor guard missing")
			}
		} else if calls != 0 || w.Body.Len() != 0 {
			t.Fatal("token type guard not first")
		}
	}
}
