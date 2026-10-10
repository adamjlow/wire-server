// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type settingsSpy struct{ writes int }

func (*settingsSpy) defaultSSO(context.Context) (*gocql.UUID, error) { return nil, nil }
func (s *settingsSpy) setDefaultSSO(context.Context, *gocql.UUID) error {
	s.writes++
	return nil
}
func (*settingsSpy) userCreated(context.Context, gocql.UUID) (*time.Time, error) {
	return nil, nil
}

func TestInvalidSettingsCannotMutatePersistentDefault(t *testing.T) {
	store := &settingsSpy{}
	handler := newHandler(Config{}, store)
	for _, body := range []string{"{}", "null", `{"default_sso_code":"synthetic-secret"}`, `{"default_sso_code":1}`, `{"default_sso_code":null} {}`} {
		req := httptest.NewRequest("PUT", "/i/sso/settings", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != 400 || store.writes != 0 || strings.Contains(response.Body.String(), "synthetic-secret") {
			t.Fatal("invalid settings mutated storage or exposed submitted input")
		}
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("PUT", "/v0/i/sso/settings", strings.NewReader(`{"default_sso_code":null}`)))
	if response.Code != 404 || store.writes != 0 {
		t.Fatal("versioned internal request bypassed ingress guard")
	}
}

func TestStoredCertificateRejectsEntityExpansionAndMalformedKeyInfo(t *testing.T) {
	for _, encoded := range []string{
		`<!DOCTYPE KeyInfo [<!ENTITY leak SYSTEM "file:///etc/passwd">]><KeyInfo xmlns="http://www.w3.org/2000/09/xmldsig#"><X509Data><X509Certificate>&leak;</X509Certificate></X509Data></KeyInfo>`,
		`<KeyInfo xmlns="http://www.w3.org/2000/09/xmldsig#"><X509Data><X509Certificate>not-a-certificate</X509Certificate></X509Data></KeyInfo>`,
		`<KeyInfo xmlns="wrong"><X509Data><X509Certificate>AA==</X509Certificate></X509Data></KeyInfo>`,
	} {
		if _, err := decodeStoredCertificate([]byte(encoded)); err == nil {
			t.Fatal("malformed stored signing certificate accepted")
		}
	}
}
