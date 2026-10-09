// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"testing"
	"time"
)

func syntheticUserRow() migrationRow {
	return migrationRow{"name": "Synthetic user", "activated": false, "writetime(activated)": int64(1700000000123456)}
}

func TestUserMigrationScalarDecodingPreservesSourceDefaultsNullsAndWriteTime(t *testing.T) {
	id, _ := postgresUUID(jobUUID)
	row := syntheticUserRow()
	row["supported_protocols"] = int32(-1)
	row["language"] = "en-legacy"
	row["handle"] = "native stored handle bypasses API grammar"
	record, eligible, err := decodeUserMigrationRow(id, row)
	if err != nil || !eligible || record.Activated || record.AccentID != 0 || record.UserType != 0 || record.Status != nil || record.Service.Valid || *record.SupportedProtocols != 3 || *record.Language != "en" || record.Handle == nil || !record.CreatedAt.Equal(time.Unix(1700000000, 123456000).UTC()) {
		t.Fatal("source scalar defaults/NULLs/microsecond time changed")
	}
	for _, required := range []string{"name", "activated", "writetime(activated)"} {
		missing := syntheticUserRow()
		missing[required] = nil
		if _, eligible, err := decodeUserMigrationRow(id, missing); err != nil || eligible {
			t.Fatal("source invalid user skip turned into fatal error or valid user")
		}
	}
	row["name"] = "" // Native Cql Name is a Text newtype, not the API's range schema.
	if _, eligible, err := decodeUserMigrationRow(id, row); err != nil || !eligible {
		t.Fatal("stored Name subjected to stricter API validation")
	}
}

func TestUserMigrationTypedFieldErrorsPrecedeMissingRequiredUserSkip(t *testing.T) {
	id, _ := postgresUUID(jobUUID)
	for column, value := range map[string]any{"password": []byte("invalid synthetic hash"), "managed_by": int32(2), "status": int32(5), "user_type": int32(3), "email": "not an email", "country": "XX", "provider": "invalid synthetic UUID", "activated": "false"} {
		row := syntheticUserRow()
		row["name"] = nil
		row[column] = value
		if _, _, err := decodeUserMigrationRow(id, row); err == nil {
			t.Fatal("malformed typed user field hidden by missing-name skip")
		}
	}
}
