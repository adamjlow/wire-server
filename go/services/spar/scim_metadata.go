// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"embed"
	"encoding/json"
	"net/http"
	"strings"
)

// These are the native public schema constants, not an interpreter or runtime.
// Source attribution and exact extraction hashes live beside the JSON files.
//
//go:embed scim-schemas/*.json
var scimSchemas embed.FS

var scimMetadataSchemas = []struct{ file, id string }{
	{"User", scimUserSchema},
	{"SPConfig", "urn:ietf:params:scim:schemas:core:2.0:ServiceProviderConfig"},
	{"Group", scimGroupSchema},
	{"Schema", "urn:ietf:params:scim:schemas:core:2.0:Schema"},
	{"ResourceType", "urn:ietf:params:scim:schemas:core:2.0:ResourceType"},
}

func scimMetadataList(resources any, count int) map[string]any {
	return map[string]any{"schemas": []string{scimListSchema}, "Resources": resources, "totalResults": count, "itemsPerPage": count, "startIndex": 1}
}

func serveSCIMMetadata(w http.ResponseWriter, r *http.Request) bool {
	path := strings.TrimPrefix(r.URL.Path, "/scim/v2/")
	if path == r.URL.Path || (path != "ServiceProviderConfig" && path != "Schemas" && path != "ResourceTypes" && !strings.HasPrefix(path, "Schemas/")) {
		return false
	}
	if strings.Contains(strings.TrimPrefix(path, "Schemas/"), "/") {
		return false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return true
	}
	var body any
	switch path {
	case "ServiceProviderConfig":
		ids := make([]string, 0, len(scimMetadataSchemas))
		for _, schema := range scimMetadataSchemas {
			ids = append(ids, schema.id)
		}
		// Preserve native advertisement, including Basic and unsupported filter.
		body = map[string]any{
			"schemas":               ids,
			"patch":                 map[string]any{"supported": true},
			"bulk":                  map[string]any{"supported": false, "maxOperations": 0, "maxPayloadSize": 0},
			"filter":                map[string]any{"supported": false, "maxResults": 0},
			"changePassword":        map[string]any{"supported": false},
			"sort":                  map[string]any{"supported": false},
			"etag":                  map[string]any{"supported": false},
			"authenticationSchemes": []any{map[string]any{"type": "httpbasic", "name": "HTTP Basic", "description": "Authentication via the HTTP Basic standard", "specUri": "https://tools.ietf.org/html/rfc7617", "documentationUri": "https://en.wikipedia.org/wiki/Basic_access_authentication"}},
		}
	case "ResourceTypes":
		body = scimMetadataList([]any{
			map[string]any{"name": "User", "endpoint": "/Users", "schema": scimUserSchema, "schemaExtensions": []any{map[string]any{"schema": richListURN, "required": false}, map[string]any{"schema": richMapURN, "required": false}}},
			map[string]any{"name": "Group", "endpoint": "/Groups", "schema": scimGroupSchema},
		}, 2)
	default:
		values := make([]json.RawMessage, 0, len(scimMetadataSchemas))
		for _, schema := range scimMetadataSchemas {
			if path != "Schemas" && path != "Schemas/"+schema.id {
				continue
			}
			data, err := scimSchemas.ReadFile("scim-schemas/" + schema.file + ".json")
			if err != nil {
				scimFailure(w, err)
				return true
			}
			values = append(values, json.RawMessage(data))
		}
		if path == "Schemas" {
			body = scimMetadataList(values, len(values))
		} else if len(values) == 1 {
			body = values[0]
		} else {
			scimFailure(w, &scimError{Status: 404, Detail: "Schema " + strings.TrimPrefix(path, "Schemas/") + " not found"})
			return true
		}
	}
	scimResponse(w, r, http.StatusOK, body)
	return true
}
