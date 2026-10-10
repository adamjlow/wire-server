// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestInternalVersionGuardPrecedesRouteAndCannotBeSpoofed(t *testing.T) {
	handler := NewHandler(Config{})
	for _, path := range []string{"/v0/i/status", "/v99/i/status", "/v/api-internal/status"} {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("X-Wire-API-Version", "19")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		var value struct{ Label, Message string }
		if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil {
			t.Fatal(err)
		}
		if response.Code != 404 || value.Label != "unsupported-version" || value.Message != "Internal APIs (`/i/...`) are not under version control" {
			t.Fatalf("wrong internal guard for %s", path)
		}
	}
}
