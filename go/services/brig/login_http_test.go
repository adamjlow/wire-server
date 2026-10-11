// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func TestPublicLoginSignedResponseAndCookie(t *testing.T) {
	for _, persistent := range []bool{false, true} {
		s, cookies, _, user := testLoginService(t)
		label := "synthetic-device"
		h := loginHTTP{service: s, refreshSuggested: func(context.Context, gocql.UUID, string) error { t.Fatal("unexpected same-label refresh"); return nil }}
		b := brigReadHandlers{login: &h}
		path := "/login"
		if persistent {
			path += "?persist=true"
		}
		request := httptest.NewRequest("POST", path, strings.NewReader(`{"email":"synthetic@example.com","password":"synthetic-password","label":"synthetic-device"}`))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		newHandler(Config{}, &b).ServeHTTP(response, request)
		if response.Code != 200 || response.Header().Get("Content-Type") != "application/json;charset=utf-8" {
			t.Fatal(response.Code, response.Body.String())
		}
		var body struct {
			User, Access, Type string
			Expires            int64
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(response.Body.Bytes(), &fields) != nil || len(fields) != 4 {
			t.Fatal("unexpected response fields", response.Body.String())
		}
		_ = json.Unmarshal(fields["user"], &body.User)
		_ = json.Unmarshal(fields["access_token"], &body.Access)
		_ = json.Unmarshal(fields["token_type"], &body.Type)
		_ = json.Unmarshal(fields["expires_in"], &body.Expires)
		if body.User != user.String() || body.Type != "Bearer" || body.Expires != 30 {
			t.Fatal(body)
		}
		got := response.Result().Cookies()
		if len(got) != 1 || got[0].Name != "zuid" || got[0].Path != "/access" || !got[0].HttpOnly || !got[0].Secure || got[0].SameSite != 0 || got[0].Expires.IsZero() == persistent {
			t.Fatal("cookie contract changed", got)
		}
		if len(cookies.cookies) != 1 || cookies.cookies[0].Label == nil || *cookies.cookies[0].Label != label {
			t.Fatal("device label not persisted")
		}
		token, err := ParseZToken(got[0].Value, "u")
		if err != nil || token.User != user.String() {
			t.Fatal("invalid cookie", err)
		}
	}
}

func TestLoginParserValidatesBothIdentifiersAndExactFieldNames(t *testing.T) {
	for _, body := range []string{
		`{"email":"bad-email","handle":"valid-handle","password":"synthetic-password"}`,
		`{"email":"synthetic@example.com","handle":"INVALID","password":"synthetic-password"}`,
		`{"handle":"valid-handle","Password":"synthetic-password"}`,
		`{"handle":"valid-handle","password":null}`,
		`{"handle":"valid-handle","password":"short"}`,
		`{"handle":"valid-handle","password":"synthetic-password","verification_code":"short"}`,
		`null`, `[]`, `{} {}`,
	} {
		if _, err := decodeLogin(strings.NewReader(body)); err == nil {
			t.Fatal("invalid login accepted", body)
		}
	}
	request, err := decodeLogin(strings.NewReader(`{"email":"synthetic@example.com","handle":"valid-handle","password":"synthetic-password","label":null,"verification_code":null}`))
	if err != nil || request.identifier.email == nil || request.label != nil || request.code != nil {
		t.Fatal("valid optional input rejected", err)
	}
}

func TestLoginErrorsPreserveRetryHeadersAndNeverLeakStorageDetail(t *testing.T) {
	for _, example := range []struct {
		err                   error
		status                int
		retry, label, message string
	}{
		{loginBlocked{seconds: 12}, 403, "12", "client-error", "Too many failed logins"},
		{cookieRetryAfter{seconds: 3}, 429, "3", "client-error", "Logins too frequent"},
		{loginCodeRequired, 403, "", "code-authentication-required", "Verification code required."},
		{errors.New("synthetic private database detail"), 500, "", "server-error", "Internal Server Error"},
	} {
		response := httptest.NewRecorder()
		loginHTTPError(response, example.err)
		var body struct{ Label, Message string }
		_ = json.Unmarshal(response.Body.Bytes(), &body)
		if response.Code != example.status || response.Header().Get("Retry-After") != example.retry || response.Header().Get("Content-Type") != "application/json" || body.Label != example.label || body.Message != example.message {
			t.Fatal(response.Code, response.Body.String())
		}
	}
}

func TestLoginInvalidQueryPrecedesBodyAndAuthentication(t *testing.T) {
	h := loginHTTP{}
	request := httptest.NewRequest(http.MethodPost, "/login?persist=invalid-synthetic", strings.NewReader(`{}`))
	response := httptest.NewRecorder()
	h.handle(response, request)
	if response.Code != 400 || response.Header().Get("Content-Type") != "" {
		t.Fatal("query error was not Servant-style literal", response.Body.String())
	}
}
