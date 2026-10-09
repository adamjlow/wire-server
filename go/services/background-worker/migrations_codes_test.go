// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"strings"
	"testing"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func syntheticCodeRow() migrationRow {
	conv, _ := gocql.ParseUUID(jobUUID)
	return migrationRow{"key": "synthetic_key_000000", "value": "synthetic_value", "conversation": conv, "ttl(value)": int32(120), "password": nil}
}

func TestCodeMigrationPreservesRemainingTTLTargetAndNullablePassword(t *testing.T) {
	for _, expired := range []bool{false, true} {
		row := syntheticCodeRow()
		if expired {
			row["ttl(value)"] = int32(0)
		}
		calls := 0
		counted, err := copyCodeRow(t.Context(), row, func(_ context.Context, sql string, args ...any) error {
			calls++
			if len(args) != 6 || args[4] != int32(120) || args[5] != "conv" || args[2].([]byte) != nil || strings.Contains(sql, "synthetic") {
				t.Fatal("code TTL, target, NULL password or bound secrets changed")
			}
			return nil
		})
		if err != nil || counted == expired || (calls == 1) == expired {
			t.Fatalf("code expiry handling changed: %v", err)
		}
	}
}

func TestCodeMigrationValidatesNativeRangeAndPasswordBeforeExpirySkip(t *testing.T) {
	for _, field := range []string{"key", "value", "conversation", "ttl(value)"} {
		row := syntheticCodeRow()
		row[field] = nil
		if _, err := decodeCodeMigration(row); err == nil {
			t.Fatal("missing required code field accepted")
		}
	}
	row := syntheticCodeRow()
	row["value"] = "short"
	if _, err := decodeCodeMigration(row); err == nil {
		t.Fatal("native range ignored")
	}
	row = syntheticCodeRow()
	row["key"] = "++++++++++++++++++++"
	if _, err := decodeCodeMigration(row); err != nil {
		t.Fatal("native CQL text incorrectly subjected to API alphabet validation")
	}
	row["ttl(value)"] = int32(0)
	row["password"] = []byte("invalid synthetic password encoding")
	if _, err := decodeCodeMigration(row); err == nil {
		t.Fatal("expired malformed password skipped before typed page decoding")
	}
}

func TestStoredPasswordsCanonicalizeWithoutKDFWork(t *testing.T) {
	cases := map[string]string{
		"$argon2id$p=1,t=2,m=8$c2FsdA$aGFzaA":             "$argon2id$v=16$m=8,t=2,p=1$c2FsdA$aGFzaA",
		"$argon2i$v=19$t=3,m=7,p=2,m=8$c2FsdA==$aGFzaA==": "$argon2i$v=19$m=8,t=3,p=2$c2FsdA$aGFzaA",
		" 014 |0x8|(1)|c2FsdA==|aGFzaA==":                 "14|8|1|c2FsdA==|aGFzaA==",
		"-1|8|1||":                                        "4294967295|8|1||",
	}
	for input, want := range cases {
		got, err := canonicalStoredPassword([]byte(input))
		if err != nil || string(got) != want {
			t.Fatal("native stored password canonicalization changed")
		}
	}
	for _, input := range []string{"$argon2id$v=20$m=8,t=2,p=1$c2FsdA$aGFzaA", "$argon2id$v=19$m=8,t=2,p=1,other=0$c2FsdA$aGFzaA", "14|8|1|c2FsdA|aGFzaA==", "14|8|1|c2FsdA==\n|aGFzaA==", "invalid", string([]byte{255})} {
		if _, err := canonicalStoredPassword([]byte(input)); err == nil {
			t.Fatal("malformed password accepted")
		}
	}
}
