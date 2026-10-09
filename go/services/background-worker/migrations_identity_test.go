// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func syntheticPasswordResetRow() migrationRow {
	user, _ := gocql.ParseUUID(jobUUID)
	return migrationRow{"key": "synthetic_key==", "code": "synthetic_code==", "user": user, "retries": 2, "timeout": time.Unix(1700000000, 0).UTC(), "ttl(code)": 120}
}

func TestPasswordResetMigrationBindsOriginalTTLAndSkipsExpiredOrLegacyRows(t *testing.T) {
	for _, variant := range []string{"valid", "expired", "missing-retries", "missing-timeout"} {
		row := syntheticPasswordResetRow()
		switch variant {
		case "expired":
			row["ttl(code)"] = 0
		case "missing-retries":
			row["retries"] = nil
		case "missing-timeout":
			row["timeout"] = nil
		}
		calls := 0
		counted, err := copyPasswordResetRow(t.Context(), row, func(_ context.Context, sql string, args ...any) error {
			calls++
			if len(args) != 6 || args[5] != int32(120) || strings.Contains(sql, "synthetic_code") || strings.Contains(sql, "synthetic_key") {
				t.Fatal("reset secrets interpolated or source remaining TTL lost")
			}
			return nil
		})
		if err != nil || counted != (variant == "valid") || (calls == 1) != (variant == "valid") {
			t.Fatalf("source %s handling changed: %v", variant, err)
		}
	}
}

func TestPasswordResetMigrationUsesNativeCQLTextAndRejectsMalformedTypedFields(t *testing.T) {
	row := syntheticPasswordResetRow()
	row["code"] = "synthetic+nonbase64" // Native Cql AsciiText bypasses alphabet validation.
	if _, err := decodePasswordResetMigration(row); err != nil {
		t.Fatal("stored CQL text subjected to API alphabet validation")
	}
	for _, field := range []string{"user", "code", "ttl(code)"} {
		bad := syntheticPasswordResetRow()
		bad[field] = nil
		if _, err := decodePasswordResetMigration(bad); err == nil {
			t.Fatal("required stored field accepted as NULL")
		}
	}
}
