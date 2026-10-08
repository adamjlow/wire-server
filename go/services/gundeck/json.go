package gundeck

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"unicode/utf8"
)

// Aeson rejects malformed UTF-8 and trailing JSON. Keep that input boundary
// instead of Go's default replacement of invalid UTF-8 in decoded strings.
func decodeJSON(input io.Reader, value any) error {
	raw, err := io.ReadAll(input)
	if err != nil {
		return err
	}
	if !utf8.Valid(raw) {
		return fmt.Errorf("invalid JSON UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err = decoder.Decode(value); err != nil {
		return err
	}
	var trailing any
	if err = decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("trailing JSON")
	}
	return nil
}
