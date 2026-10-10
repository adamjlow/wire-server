// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func TestReauthenticationVerificationEnforcement(t *testing.T) {
	team := gocql.UUID{}
	email := "synthetic@example.test"
	for _, test := range []struct {
		name                                      string
		body                                      string
		required, saml, activated, noEmail, match bool
		status, codeCalls                         int
	}{
		{"required missing", `{"action":"login"}`, true, false, true, false, false, 403, 0},
		{"incorrect code", `{"action":"login","verification_code":"123456"}`, true, false, true, false, false, 403, 1},
		{"correct code", `{"action":"login","verification_code":"123456"}`, true, false, true, false, true, 200, 1},
		{"unverified email", `{"action":"login","verification_code":"123456"}`, true, false, false, false, true, 403, 0},
		{"missing email", `{"action":"login","verification_code":"123456"}`, true, false, true, true, true, 403, 0},
		{"feature disabled", `{"action":"login"}`, false, false, true, false, false, 200, 0},
		{"SAML exemption", `{"action":"login"}`, true, true, true, false, false, 200, 0},
		{"no action", `{}`, true, false, true, false, false, 200, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			user := &storedUser{Team: &team, Activated: test.activated, Email: &email}
			if test.noEmail {
				user.Email = nil
			}
			if test.saml {
				user.SSO = json.RawMessage(`{"tenant":"https://sso.example.test","subject":"synthetic-subject"}`)
			}
			codeCalls := 0
			h := reauthenticationHandler{
				passwords:           authenticationService{store: &memoryAuthenticationStore{record: &userAuthentication{}}},
				accounts:            func(context.Context, gocql.UUID) (*storedUser, error) { return user, nil },
				verificationEnabled: func(context.Context, string, string) (bool, error) { return test.required, nil },
				verifyCode: func(_ context.Context, actualEmail, action, code string) (bool, error) {
					codeCalls++
					if actualEmail != email || action != "login" || code != "123456" {
						t.Fatal("incorrect code storage arguments")
					}
					return test.match, nil
				},
			}
			req := httptest.NewRequest("GET", "/i/users/00000000-0000-0000-0000-000000000000/reauthenticate", strings.NewReader(test.body))
			req.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			h.handle(response, req, team.String())
			if response.Code != test.status || codeCalls != test.codeCalls {
				t.Fatalf("status=%d code calls=%d", response.Code, codeCalls)
			}
		})
	}
}

func TestReauthenticationRejectsPasswordBeforeVerification(t *testing.T) {
	checks := 0
	h := reauthenticationHandler{
		passwords: authenticationService{store: &memoryAuthenticationStore{record: &userAuthentication{Status: 1}}},
		accounts:  func(context.Context, gocql.UUID) (*storedUser, error) { checks++; return nil, nil },
	}
	req := httptest.NewRequest("GET", "/", strings.NewReader(`{"action":"login"}`))
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	h.handle(response, req, gocql.UUID{}.String())
	if response.Code != 403 || checks != 0 || !strings.Contains(response.Body.String(), `"suspended"`) {
		t.Fatal("verification must follow successful reauthentication")
	}
	response = httptest.NewRecorder()
	authenticationHTTPError(response, passwordRateExceeded{1999999})
	if response.Code != 429 || response.Header().Get("Retry-After") != "1" || response.Header().Get("Content-Type") != "application/json" {
		t.Fatal("native retry header truncation changed")
	}
}

func TestStoredSAMLIdentityDoesNotConfuseSCIMOrInactiveUsers(t *testing.T) {
	for _, test := range []struct {
		identity                 string
		activated, want, invalid bool
	}{
		{`{"tenant":"https://sso.example.test","subject":"synthetic"}`, true, true, false},
		{`{"scim_external_id":"synthetic"}`, true, false, false},
		{`{"tenant":"https://sso.example.test","subject":"synthetic"}`, false, false, false},
		{`{"tenant":"https://sso.example.test","subject":"synthetic","scim_external_id":"synthetic"}`, true, false, true},
		{`{"tenant":"relative","subject":"synthetic"}`, true, false, true},
		{`{"tenant":1,"subject":"synthetic"}`, true, false, true},
	} {
		got, err := storedSAML(&storedUser{Activated: test.activated, SSO: json.RawMessage(test.identity)})
		if got != test.want || (err != nil) != test.invalid {
			t.Fatal("incorrect SAML classification", got, err)
		}
	}
	response := httptest.NewRecorder()
	authenticationHTTPError(response, errors.New("synthetic private failure"))
	if strings.Contains(response.Body.String(), "private") {
		t.Fatal("storage diagnostics leaked")
	}
}

func TestEmailVerificationKeyNativeTrustedDomainRules(t *testing.T) {
	key := func(email string) string {
		t.Helper()
		value, err := emailVerificationKey(email)
		if err != nil {
			t.Fatal(err)
		}
		if len(value) != 20 {
			t.Fatal("key length changed")
		}
		return value
	}
	if key("Synthetic+tag@example.test") != key("synthetic@example.test") {
		t.Fatal("untrusted domain plus suffix must be stripped")
	}
	if key("Synthetic+tag@wire.com") == key("synthetic@wire.com") {
		t.Fatal("trusted domain plus suffix must be retained")
	}
	if key("Synthetic+tag@WIRE.COM") != key("synthetic@wire.com") {
		t.Fatal("native trusted comparison precedes lowercase")
	}
	for _, value := range []string{"123456", "synthetic==", "_a-B12"} {
		if !validVerificationValue(value) {
			t.Fatal("native URL-safe code rejected")
		}
	}
	for _, value := range []string{"12345", strings.Repeat("a", 21), "abc+12", "private\n"} {
		if validVerificationValue(value) {
			t.Fatal("invalid code accepted")
		}
	}
}
