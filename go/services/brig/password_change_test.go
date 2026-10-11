// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/apache/cassandra-gocql-driver/v2"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestPasswordChangeNativeErrorMedia(t *testing.T) {
	for _, tc := range []struct {
		label  string
		status int
	}{{"no-identity", 403}, {"invalid-credentials", 403}, {"password-must-differ", 409}} {
		t.Run(tc.label, func(t *testing.T) {
			current := "synthetic-hash"
			s := passwordChangeService{
				activated: func(context.Context, gocql.UUID) (bool, error) { return tc.label != "no-identity", nil },
				current:   func(context.Context, gocql.UUID) (*string, error) { return &current, nil },
				hash:      func(context.Context, passwordRateKey, string, int) (string, error) { return "synthetic-new-hash", nil },
				verify: func(context.Context, passwordRateKey, string, string) (bool, bool, error) {
					return tc.label == "password-must-differ", false, nil
				},
				write: func(context.Context, gocql.UUID, string) error {
					t.Fatal("rejected request wrote password")
					return nil
				},
				revoke: func(context.Context, gocql.UUID) error { t.Fatal("rejected request revoked cookies"); return nil },
			}
			r := httptest.NewRequest("PUT", "/self/password", strings.NewReader(`{"old_password":"synthetic-old-password","new_password":"synthetic-new-password"}`))
			r.Header.Set("Z-User", "10000000-0000-0000-0000-000000000001")
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			s.handle(w, r)
			var body struct {
				Code  int    `json:"code"`
				Label string `json:"label"`
			}
			if w.Code != tc.status || w.Header().Get("Content-Type") != "application/json;charset=utf-8" || json.Unmarshal(w.Body.Bytes(), &body) != nil || body.Code != tc.status || body.Label != tc.label {
				t.Fatal("native password change error contract changed", w.Code, w.Header(), w.Body.String())
			}
		})
	}
}

func TestPasswordChangeNativeOrderAndCookieRules(t *testing.T) {
	for _, mode := range []string{"inactive", "first", "missing-old", "wrong-old", "same", "success", "rate", "write-failure", "revoke-failure"} {
		t.Run(mode, func(t *testing.T) {
			var calls []string
			user := gocql.UUID{1}
			old := "old-password"
			current := "synthetic-hash"
			failure := errors.New("synthetic store failure")
			s := passwordChangeService{
				activated: func(context.Context, gocql.UUID) (bool, error) { return mode != "inactive", nil },
				current: func(context.Context, gocql.UUID) (*string, error) {
					calls = append(calls, "read")
					if mode == "first" {
						return nil, nil
					}
					return &current, nil
				},
				hash: func(_ context.Context, key passwordRateKey, _ string, min int) (string, error) {
					calls = append(calls, "hash")
					if key.kind != "user" || key.id != user.String() || min != 8 {
						t.Fatal("native rate/length key changed")
					}
					if mode == "rate" {
						return "", passwordRateExceeded{micros: 1000000}
					}
					return "new-hash", nil
				},
				verify: func(_ context.Context, _ passwordRateKey, encoded, plain string) (bool, bool, error) {
					calls = append(calls, "verify")
					if encoded != current {
						t.Fatal("unexpected current hash")
					}
					return (plain == old && mode != "wrong-old") || mode == "same", true, nil
				},
				write: func(_ context.Context, _ gocql.UUID, encoded string) error {
					calls = append(calls, "write")
					if encoded != "new-hash" {
						t.Fatal("hash not used")
					}
					if mode == "write-failure" {
						return failure
					}
					return nil
				},
				revoke: func(context.Context, gocql.UUID) error {
					calls = append(calls, "revoke")
					if mode == "revoke-failure" {
						return failure
					}
					return nil
				},
			}
			request := passwordChangeRequest{old: &old, new: "new-password"}
			if mode == "missing-old" || mode == "rate" {
				request.old = nil
			}
			err := s.change(context.Background(), user, request)
			expected := []string{"read", "hash", "verify", "verify", "write", "revoke"}
			switch mode {
			case "inactive":
				expected = nil
				if !errors.Is(err, errPasswordChangeIdentity) {
					t.Fatal("inactive account accepted")
				}
			case "first":
				expected = []string{"read", "hash", "write"}
				if err != nil {
					t.Fatal("initial password rejected", err)
				}
			case "missing-old":
				expected = []string{"read", "hash"}
				if !errors.Is(err, errPasswordChangeCurrent) {
					t.Fatal("missing current password accepted")
				}
			case "wrong-old":
				expected = []string{"read", "hash", "verify"}
				if !errors.Is(err, errPasswordChangeCurrent) {
					t.Fatal("wrong current password accepted")
				}
			case "same":
				expected = []string{"read", "hash", "verify", "verify"}
				if !errors.Is(err, errPasswordChangeSame) {
					t.Fatal("unchanged password accepted")
				}
			case "rate":
				expected = []string{"read", "hash"}
				var limit passwordRateExceeded
				if !errors.As(err, &limit) {
					t.Fatal("native hash-before-auth error precedence changed")
				}
			case "write-failure":
				expected = expected[:5]
				if !errors.Is(err, failure) {
					t.Fatal("write failure hidden")
				}
			case "revoke-failure":
				if !errors.Is(err, failure) {
					t.Fatal("revoke failure hidden")
				}
			case "success":
				if err != nil {
					t.Fatal(err)
				}
			}
			if !reflect.DeepEqual(calls, expected) {
				t.Fatal("native password work/revocation order changed", calls)
			}
		})
	}
}

func TestPasswordChangeSchemaAndHEADRouting(t *testing.T) {
	for _, body := range []string{`{}`, `{"new_password":null}`, `{"new_password":"1234567"}`, `{"old_password":"12345","new_password":"12345678"}`, `{"old_password":123,"new_password":"12345678"}`, `{"new_password":"12345678"} {}`} {
		if _, err := decodePasswordChange(strings.NewReader(body)); err == nil {
			t.Fatal("invalid password payload accepted")
		}
	}
	if request, err := decodePasswordChange(strings.NewReader(`{"old_password":null,"new_password":"12345678"}`)); err != nil || request.old != nil {
		t.Fatal("optional current password changed")
	}
	present := false
	s := passwordChangeService{current: func(context.Context, gocql.UUID) (*string, error) {
		if present {
			value := "synthetic-hash"
			return &value, nil
		}
		return nil, nil
	}}
	handler := newHandler(Config{}, &brigReadHandlers{passwordChange: &s})
	for _, expected := range []int{404, 200} {
		r := httptest.NewRequest("HEAD", "/v19/self/password", nil)
		r.Header.Set("Z-User", "10000000-0000-0000-0000-000000000001")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != expected || w.Body.Len() != 0 {
			t.Fatal("native HEAD password existence changed")
		}
		present = true
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("HEAD", "/self/password", nil))
	if w.Code != 400 {
		t.Fatal("missing gateway user accepted")
	}
}
