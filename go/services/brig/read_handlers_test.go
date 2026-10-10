// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"net/http/httptest"
	"testing"
)

func TestReadRoutesRejectInvalidIdentityBeforeStorageAccess(t *testing.T) {
	// No database handles are installed: an invalid request must never reach
	// them. Z-User remains the original trusted gateway boundary.
	service := brigReadHandlers{}
	handler := newHandler(Config{}, &service)
	for _, path := range []string{"/self", "/cookies", "/v1/self", "/v1/cookies"} {
		for _, header := range []string{"", "not-a-uuid", "00000000000000000000000000000001", "00000000-0000-0000-0000-000000000001.extra"} {
			request := httptest.NewRequest("GET", path, nil)
			if header != "" {
				request.Header.Set("Z-User", header)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != 400 {
				t.Fatalf("invalid identity accepted at %s", path)
			}
		}
	}
}
