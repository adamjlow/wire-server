// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestMetadataIsPublicAndExtensionLookupStaysUnsupported(t *testing.T) {
	handler := NewHandler(Config{})
	for _, schema := range scimMetadataSchemas {
		req := httptest.NewRequest("GET", "/scim/v2/Schemas/"+schema.id, nil)
		req.Header.Set("Authorization", "Bearer invalid")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		var value struct{ ID string }
		if json.Unmarshal(response.Body.Bytes(), &value) != nil || response.Code != 200 || value.ID != schema.id {
			t.Fatalf("public schema %s did not match", schema.id)
		}
	}
	for _, id := range []string{richListURN, richMapURN, "unknown"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest("GET", "/scim/v2/Schemas/"+id, nil))
		var value struct{ Status, Detail string }
		if json.Unmarshal(response.Body.Bytes(), &value) != nil || response.Code != 404 || value.Status != "404" || value.Detail != "Schema "+id+" not found" {
			t.Fatalf("unsupported extension %s changed native failure", id)
		}
	}
}
