// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"
)

type userMigrationRichField struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

func decodeUserMigrationRichInfo(blob []byte) (json.RawMessage, error) {
	invalid := errors.New("invalid stored user rich info")
	if !utf8.Valid(blob) {
		return nil, invalid
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(blob, &fields) != nil || fields == nil {
		return nil, invalid
	}
	// Native ciObject uses case-insensitive keys for the outer version/fields.
	var version, rawFields json.RawMessage
	for key, value := range fields {
		switch strings.ToLower(key) {
		case "version":
			version = value
		case "fields":
			rawFields = value
		}
	}
	var v json.Number
	if json.Unmarshal(version, &v) != nil {
		return nil, invalid
	}
	numeric := strings.TrimSpace(string(version))
	if numeric == "" || numeric[0] == '"' {
		return nil, invalid
	}
	coefficient, _, _ := strings.Cut(strings.ToLower(numeric), "e")
	coefficient = strings.TrimPrefix(coefficient, "-")
	coefficient = strings.ReplaceAll(coefficient, ".", "")
	if coefficient == "" || strings.Trim(coefficient, "0") != "" {
		return nil, invalid
	}
	var raw []map[string]json.RawMessage
	if json.Unmarshal(rawFields, &raw) != nil || raw == nil {
		return nil, invalid
	}
	seen := make(map[string]bool, len(raw))
	valid := make([]userMigrationRichField, 0, len(raw))
	for _, field := range raw {
		var typ, value string
		if string(field["type"]) == "null" || string(field["value"]) == "null" || json.Unmarshal(field["type"], &typ) != nil || json.Unmarshal(field["value"], &value) != nil {
			return nil, invalid
		}
		key := foldedDomainKey(typ)
		// Native duplicate check runs before normalization removes empty values.
		if seen[key] {
			return nil, invalid
		}
		seen[key] = true
		if value != "" {
			valid = append(valid, userMigrationRichField{Type: typ, Value: value})
		}
	}
	result, err := json.Marshal(struct {
		Version int                      `json:"version"`
		Fields  []userMigrationRichField `json:"fields"`
	}{Fields: valid})
	if err != nil {
		return nil, invalid
	}
	return result, nil
}

func decodeUserMigrationPicture(value any) (json.RawMessage, error) {
	if value == nil {
		return nil, nil
	}
	invalid := errors.New("invalid stored user picture")
	// Native Pict.fromCql decodes each blob as an Object. Its schema's 0..10
	// API limit is not imposed by the Cassandra decoder.
	var blobs [][]byte
	switch value := value.(type) {
	case [][]byte:
		blobs = value
	case []any:
		for _, entry := range value {
			blob, ok := entry.([]byte)
			if !ok {
				return nil, invalid
			}
			blobs = append(blobs, blob)
		}
	default:
		// Native Pict.fromCql of a non-list produces noPict, not an error.
		return json.RawMessage("[]"), nil
	}
	objects := make([]json.RawMessage, 0, len(blobs))
	for _, blob := range blobs {
		var object map[string]json.RawMessage
		if !utf8.Valid(blob) || json.Unmarshal(blob, &object) != nil || object == nil {
			return nil, invalid
		}
		objects = append(objects, json.RawMessage(blob))
	}
	result, err := json.Marshal(objects)
	if err != nil {
		return nil, invalid
	}
	return result, nil
}
