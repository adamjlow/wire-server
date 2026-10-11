// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http/httptest"
	"net/mail"
	"strings"
	"testing"
)

func TestPasswordResetRequestSchemaAndLegacyRoutes(t *testing.T) {
	for _, c := range []struct {
		body  string
		begin bool
	}{
		{`{}`, true}, {`{"email":"synthetic@example.com","phone":"any"}`, true}, {`{"email":"invalid"}`, true},
		{`{"key":"valid-key","email":"invalid","code":"valid-code","password":"new-password"}`, false},
		{`{"key":"valid-key","phone":"+123","code":"valid-code","password":"new-password"}`, false},
		{`{"key":"valid-key","code":"valid-code","password":"123456"}`, false},
		{`{"key":"valid-key","code":"bad code","password":"new-password"}`, false},
		{`{"key":"valid-key","code":"valid-code","password":"12345678"} {}`, false},
	} {
		if _, err := decodePasswordReset(strings.NewReader(c.body), c.begin, nil); err == nil {
			t.Fatal("invalid reset payload accepted")
		}
	}
	request, err := decodePasswordReset(strings.NewReader(`{"key":"valid-key","email":"synthetic@example.com","phone":"+12345678","code":"valid-code","password":"new-password"}`), false, nil)
	if err != nil || request.key == nil || request.email == nil || request.phone == nil {
		t.Fatal("native identity precedence/field validation changed", err)
	}
	s, _, _ := resetFixture(t)
	business := brigReadHandlers{passwordReset: &passwordResetHTTP{service: s}}
	handler := newHandler(Config{}, &business)
	for _, c := range []struct {
		path, body string
		status     int
	}{
		{"/password-reset", `{"phone":"legacy arbitrary text"}`, 400},
		{"/password-reset", `{"email":"synthetic@example.com"}`, 201},
		{"/v7/password-reset/unknown-key", `{"code":"unknown-code","password":"new-password","email":123}`, 400},
		{"/v8/password-reset/unknown-key", `{"code":"unknown-code","password":"new-password"}`, 404},
		{"/v19/password-reset/complete", `{"phone":"+12345678","code":"valid-code","password":"new-password"}`, 400},
	} {
		r := httptest.NewRequest("POST", c.path, strings.NewReader(c.body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != c.status {
			t.Fatal("reset route/version contract changed", c.path, w.Code)
		}
	}
}

func TestPasswordResetActualSMTPNativeTemplate(t *testing.T) {
	fixture := newSMTPFixture(t, "plain")
	c := nativeMailTestConfig(t)
	smtp := fixture.config()
	c.Other["emailSMS"].(map[string]any)["email"] = map[string]any{"smtpEndpoint": map[string]any{"host": smtp.endpoint.Host, "port": smtp.endpoint.Port}, "smtpConnType": "plain"}
	delivery, err := newRegistrationMailDelivery(context.Background(), c, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer delivery.close()
	s, _, _ := resetFixture(t)
	s.mail = delivery.passwordReset
	email := "synthetic@example.com"
	if err := s.begin(context.Background(), passwordResetRequest{email: &email}); err != nil {
		t.Fatal(err)
	}
	raw := <-fixture.received
	parsed, err := mail.ReadMessage(bytes.NewBufferString(raw))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Header.Get("To") != "<synthetic@example.com>" || parsed.Header.Get("X-Zeta-Purpose") != "PasswordReset" || parsed.Header.Get("X-Zeta-Code") != "synthetic-code" || parsed.Header.Get("X-Zeta-Key") == "" {
		t.Fatal("native reset mail envelope/processing fields changed")
	}
	media, params, err := mime.ParseMediaType(parsed.Header.Get("Content-Type"))
	if err != nil || media != "multipart/alternative" {
		t.Fatal("native reset MIME changed")
	}
	reader := multipart.NewReader(parsed.Body, params["boundary"])
	for _, separator := range []string{"&code=", "&amp;code="} {
		part, err := reader.NextPart()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(part)
		if err != nil || !strings.Contains(string(body), "https://example.com/reset/?key=") || !strings.Contains(string(body), separator+"synthetic-code") {
			t.Fatal("native URL substitution or HTML escaping changed")
		}
	}
	pair, err := s.pair(context.Background(), email)
	if err != nil {
		t.Fatal(err)
	}
	business := brigReadHandlers{passwordReset: &passwordResetHTTP{service: s}}
	for _, method := range []string{"GET", "HEAD"} {
		w := httptest.NewRecorder()
		newHandler(Config{}, &business).ServeHTTP(w, httptest.NewRequest(method, "/i/users/password-reset-code?email=synthetic@example.com", nil))
		if w.Code != 200 {
			t.Fatal("internal reset lookup failed")
		}
		if method == "GET" {
			var actual map[string]string
			if json.Unmarshal(w.Body.Bytes(), &actual) != nil || actual["key"] != pair["key"] || actual["code"] != pair["code"] {
				t.Fatal("internal reset pair changed")
			}
		} else if w.Body.Len() != 0 {
			t.Fatal("HEAD exposed reset body")
		}
	}
}
