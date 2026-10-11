// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strings"
)

func federationVersionHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	values, present := r.Header["Wire-Origin-Domain"]
	if !present || len(values) == 0 {
		routeParseError(w, "Header Wire-Origin-Domain is required")
		return
	}
	if _, err := parseEmailDomain(values[0]); err != nil {
		routeParseError(w, "Error parsing header Wire-Origin-Domain failed: invalid domain")
		return
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(media, "application/json") {
		w.WriteHeader(415)
		return
	}
	decoder := json.NewDecoder(r.Body)
	var raw json.RawMessage
	var trailing any
	if decoder.Decode(&raw) != nil || decoder.Decode(&trailing) != io.EOF || !validRegistrationJSONText(raw) {
		wireError(w, 400, "bad-request", "Invalid federation version request")
		return
	}
	// Native input is Aeson's unit type, which ignores a successfully decoded
	// JSON value. The legacy supported field stays [0,1] for old negotiation.
	readJSON(w, r, map[string]any{"supported_versions": []int{0, 1, 2, 3, 4}, "supported": []int{0, 1}})
}
