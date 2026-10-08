package gundeck

import (
	"bytes"
	"testing"
)

func TestJSONRejectsTrailingValuesAndMalformedUTF8(t *testing.T) {
	for _, raw := range [][]byte{[]byte(`{} {}`), []byte(`{} trailing`), {'{', '"', 'k', '"', ':', '"', 0xff, '"', '}'}} {
		var value map[string]any
		if err := decodeJSON(bytes.NewReader(raw), &value); err == nil {
			t.Fatal("invalid JSON was accepted")
		}
	}
	var value map[string]any
	if err := decodeJSON(bytes.NewReader([]byte("{\"ok\":true}\n")), &value); err != nil {
		t.Fatal("valid JSON rejected", err)
	}
}
