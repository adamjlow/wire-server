// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeActivateValidatesBothTargetsAndRequiredFields(t *testing.T) {
	for _, body := range []string{
		`{"key":"key","email":"invalid-email","code":"code","dryrun":false}`,
		`{"key":"bad key","email":"synthetic@example.com","code":"code","dryrun":false}`,
		`{"key":"key","code":"code"}`, `{"key":"key","code":"code","dryrun":null}`,
		`{"key":"key","code":"code","dryrun":"false"}`, `{"Key":"key","code":"code","dryrun":false}`,
		`{"key":"key","code":null,"dryrun":false}`, `{"key":"key","code":"code","dryrun":false} {}`,
	} {
		if _, err := decodeActivate(strings.NewReader(body)); err == nil {
			t.Fatal("invalid activation body accepted")
		}
	}
	request, err := decodeActivate(strings.NewReader(`{"key":"key","email":"synthetic@example.com","code":"code","dryrun":true,"unknown":1}`))
	if err != nil || request.key != "key" || request.code != "code" || !request.dryrun {
		t.Fatal("native target precedence changed", err)
	}
	request, err = decodeActivate(strings.NewReader(`{"email":"synthetic@example.com","code":"code","dryrun":false}`))
	key, _ := activationEmailKey("synthetic@example.com")
	if err != nil || request.key != key {
		t.Fatal("email activation key changed", err)
	}
}
func TestActivationHTTPDryrunAndInvalidCode(t *testing.T) {
	for _, scenario := range []struct {
		name                   string
		domainError, errorCode bool
		status                 int
		label                  string
	}{
		{name: "dryrun", status: 200}, {name: "code", errorCode: true, status: 404, label: "invalid-code"},
		{name: "domain", domainError: true, status: 403, label: "condition-failed"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			handler := activationHTTP{service: activationAPI{
				codes: apiCodeLookup{func(context.Context, string, string) (*activationRecord, error) {
					if scenario.errorCode {
						return nil, errInvalidActivationCode
					}
					return &activationRecord{email: "synthetic@example.com"}, nil
				}},
				guardDomain: func(context.Context, string) error {
					if scenario.domainError {
						return errDomainRegistrationSSO
					}
					return nil
				},
			}}
			request := httptest.NewRequest("POST", "/activate", strings.NewReader(`{"key":"synthetic","code":"000000","dryrun":true}`))
			writer := httptest.NewRecorder()
			handler.handle(writer, request)
			if writer.Code != scenario.status {
				t.Fatalf("status %d", writer.Code)
			}
			if scenario.label != "" {
				var body map[string]any
				if json.Unmarshal(writer.Body.Bytes(), &body) != nil || body["label"] != scenario.label {
					t.Fatal("native error changed")
				}
			} else if writer.Body.Len() != 0 {
				t.Fatal("dryrun response not empty")
			}
		})
	}
}

func TestInternalActivationPairHTTPValidatesBeforeStorage(t *testing.T) {
	h := activationHTTP{lookupPair: func(context.Context, string) (map[string]string, error) {
		t.Fatal("invalid request reached storage")
		return nil, nil
	}}
	for _, c := range []struct {
		method, path string
		status       int
	}{
		{"POST", "/i/users/activation-code?email=synthetic@example.com", 405},
		{"GET", "/i/users/activation-code", 400},
		{"GET", "/i/users/activation-code?email=not-an-address", 400},
	} {
		w := httptest.NewRecorder()
		h.handleInternalPair(w, httptest.NewRequest(c.method, c.path, nil))
		if w.Code != c.status {
			t.Fatal("wrong internal pair rejection", w.Code)
		}
	}
	for _, found := range []bool{false, true} {
		h.lookupPair = func(_ context.Context, email string) (map[string]string, error) {
			if email != "synthetic@example.com" {
				t.Fatal("email parser changed")
			}
			if !found {
				return nil, nil
			}
			return map[string]string{"key": "synthetic-key", "code": "123456"}, nil
		}
		w := httptest.NewRecorder()
		h.handleInternalPair(w, httptest.NewRequest("GET", "/i/users/activation-code?email=synthetic@example.com", nil))
		if found {
			var pair map[string]string
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &pair) != nil || len(pair) != 2 || pair["code"] != "123456" {
				t.Fatal("pair response changed")
			}
		} else if w.Code != 404 || !strings.Contains(w.Body.String(), "Activation key not found.") {
			t.Fatal("missing pair contract changed")
		}
	}
}
