// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func TestAccountDeletionHTTPAdmissionAndNativeResponses(t *testing.T) {
	id := gocql.TimeUUID()
	email := "synthetic@example.com"
	user := storedUser{ID: id, Email: &email, Activated: true}
	var steps []string
	h := accountDeletionHTTP{service: deletionTestService(user, &steps, "")}
	request := func(method, path, body, media, header string) *httptest.ResponseRecorder {
		t.Helper()
		steps = nil
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if media != "" {
			r.Header.Set("Content-Type", media)
		}
		if header != "" {
			r.Header.Set("Z-User", header)
		}
		w := httptest.NewRecorder()
		if !h.handle(w, r, path) {
			t.Fatal("deletion route not handled")
		}
		return w
	}
	for _, test := range []struct {
		body, media, header string
		status              int
	}{
		{`{}`, "application/json", "", 400},
		{`{}`, "text/plain", id.String(), 415},
		{`[]`, "application/json", id.String(), 400},
		{`{"password":"short"}`, "application/json", id.String(), 400},
		{`{"password":7}`, "application/json", id.String(), 400},
		{`{} {}`, "application/json", id.String(), 400},
	} {
		w := request(http.MethodDelete, "/self", test.body, test.media, test.header)
		if w.Code != test.status || len(steps) != 0 {
			t.Fatal("admission effects", w.Code, steps)
		}
	}
	w := request(http.MethodDelete, "/self", `{"Password":"ignored-unknown-field"}`, "application/json", id.String())
	if w.Code != 202 || strings.TrimSpace(w.Body.String()) != `{"expires_in":600}` || !reflect.DeepEqual(steps, []string{"code"}) {
		t.Fatal("pending response", w.Code, w.Body, steps)
	}
	h.service.sendCode = func(context.Context, storedUser, string) (int64, error) { return 0, deletionCodePending{83} }
	w = request(http.MethodDelete, "/self", `{}`, "application/json", id.String())
	if w.Code != 403 || !strings.Contains(w.Body.String(), `"expires_in":83`) || !strings.Contains(w.Body.String(), `"label":"pending-delete"`) {
		t.Fatal("pending error", w.Code, w.Body)
	}
	h.service.sendCode = func(context.Context, storedUser, string) (int64, error) { return 0, deletionCodeThrottled{19} }
	w = request(http.MethodDelete, "/self", `{}`, "application/json", id.String())
	if w.Code != 429 || w.Header().Get("Retry-After") != "19" {
		t.Fatal("throttle projection", w.Code, w.Header())
	}
	deleted := int32(2)
	user.Status = &deleted
	h.service = deletionTestService(user, &steps, "")
	w = request(http.MethodDelete, "/self", `{}`, "application/json", id.String())
	if w.Code != 200 || w.Body.Len() != 0 || len(steps) != 0 {
		t.Fatal("deleted self response", w.Code, steps)
	}
	for _, body := range []string{`{}`, `{"Key":"01234567890123456789","code":"123456"}`, `{"key":"short","code":"123456"}`, `{"key":"01234567890123456789","code":"12345"}`, `{"key":"01234567890123456789","code":"bad!value"}`} {
		w = request(http.MethodPost, "/delete", body, "application/json", "")
		if w.Code != 400 {
			t.Fatal("invalid code admitted", w.Code, body)
		}
	}
	h.verification = accountDeletionVerification{codes: accountDeletionCodes{
		lookup: func(context.Context, string) (*deletionVerificationCode, error) {
			steps = append(steps, "lookup")
			return &deletionVerificationCode{account: &id, value: "123456", retries: 1}, nil
		},
		insert: func(context.Context, deletionVerificationCode) error { t.Fatal("successful code updated"); return nil },
		remove: func(context.Context, string) error { steps = append(steps, "remove-code"); return nil },
	}, get: func(context.Context, gocql.UUID) (*storedUser, error) { return nil, nil }, execute: h.service.execute}
	w = request(http.MethodPost, "/delete", `{"key":"01234567890123456789","code":"123456"}`, "application/json", "")
	if w.Code != 200 || w.Body.Len() != 0 || !reflect.DeepEqual(steps, []string{"lookup", "remove-code"}) {
		t.Fatal("verification response", w.Code, steps)
	}
}

func TestAccountDeletionEnsureChecksBeforeReplay(t *testing.T) {
	id := gocql.TimeUUID()
	deleted := int32(2)
	user := storedUser{ID: id, Status: &deleted}
	var steps []string
	h := accountDeletionHTTP{service: deletionTestService(user, &steps, "")}
	h.keys = func(context.Context, gocql.UUID) ([]string, error) {
		steps = append(steps, "check-properties")
		return nil, nil
	}
	h.service.clients = func(context.Context, gocql.UUID) ([]storedClient, error) {
		steps = append(steps, "check-clients")
		return nil, nil
	}
	dirty := false
	h.count = func(context.Context, gocql.UUID) (int64, error) {
		steps = append(steps, "check-connections")
		if dirty {
			return 1, nil
		}
		return 0, nil
	}
	h.cookies = func(context.Context, gocql.UUID) ([]sessionCookie, error) {
		steps = append(steps, "check-cookies")
		return nil, nil
	}
	if already, err := h.ensure(context.Background(), id, "r"); err != nil || !already || !reflect.DeepEqual(steps, []string{"check-properties", "check-clients", "check-connections", "check-cookies"}) {
		t.Fatal("clean tombstone", steps, err)
	}
	steps = nil
	dirty = true
	if already, err := h.ensure(context.Background(), id, "r"); err != nil || already || len(steps) < 5 || steps[4] != "properties" {
		t.Fatal("dirty tombstone not replayed", steps, err)
	}
	steps = nil
	r := httptest.NewRequest(http.MethodDelete, "/i/users/"+id.String(), nil)
	w := httptest.NewRecorder()
	if !h.handle(w, r, r.URL.Path) || w.Code != 202 {
		t.Fatal("internal replay status", w.Code)
	}
	h.service.get = func(context.Context, gocql.UUID) (*storedUser, error) { return nil, nil }
	w = httptest.NewRecorder()
	h.handle(w, r, r.URL.Path)
	if w.Code != 404 || !strings.Contains(w.Body.String(), `"label":"not-found"`) {
		t.Fatal("missing internal user", w.Code, w.Body)
	}
}
