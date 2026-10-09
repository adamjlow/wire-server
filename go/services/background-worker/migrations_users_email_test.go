// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestUserMigrationEmailReusesOriginalLiteralExamples(t *testing.T) {
	raw, err := os.ReadFile("testdata/native-email-examples.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Value string `json:"value"`
			Valid bool   `json:"valid"`
		} `json:"source_cases"`
	}
	if json.Unmarshal(raw, &fixture) != nil || len(fixture.Cases) == 0 {
		t.Fatal("original email fixtures missing")
	}
	for i, example := range fixture.Cases {
		canonical, err := canonicalUserMigrationEmail(example.Value)
		if (err == nil) != example.Valid {
			t.Fatalf("original email example%d validity changed", i)
		}
		if err == nil {
			again, err := canonicalUserMigrationEmail(canonical)
			if err != nil || again != canonical {
				t.Fatalf("original idempotence expectation failed at example%d", i)
			}
		}
	}
}

func TestUserMigrationEmailCanonicalizesNativeCommentsWhitespaceAndQuotedLocalParts(t *testing.T) {
	for input, want := range map[string]string{
		"spaces. are. allowed@example.com":              "spaces.are.allowed@example.com",
		"(synthetic)user(comment)@(domain)example.com.": "user@example.com",
		`"synthetic name"@example.com`:                  `"syntheticname"@example.com`,
		`"synthetic\ name"@example.com`:                 `"synthetic\ name"@example.com`,
		"user@[12.34.56.78]":                            "user@[12.34.56.78]",
		`""@1`:                                          `""@1`,
	} {
		got, err := canonicalUserMigrationEmail(input)
		if err != nil || got != want {
			t.Fatal("native stored email canonicalization changed")
		}
	}
	for _, input := range []string{strings.Repeat("a", 65) + "@example.com", "user<user@example.com>", "user@example.com extra", "user@-example.com", "user@example-.com", "u.ser..@example.com"} {
		if _, err := canonicalUserMigrationEmail(input); err == nil {
			t.Fatal("malformed native email accepted")
		}
	}
}
