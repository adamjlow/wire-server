// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// This decodes the scalar/password/picture fields of UserRowCass. The caller
// must still decode SSO, assets and related service/claim/rich-info rows before
// acting on eligible: source typed decoding precedes invalid-user skipping.
func decodeUserMigrationRow(id pgtype.UUID, row migrationRow) (record userMigrationRecord, eligible bool, err error) {
	if !id.Valid {
		return record, false, invalidUserMigrationRow()
	}
	record.ID = id
	for column, target := range map[string]**string{"handle": &record.Handle, "text_status": &record.TextStatus} {
		value, err := userMigrationNullableString(row[column])
		if err != nil {
			return record, false, err
		}
		*target = value
	}
	name, err := userMigrationNullableString(row["name"])
	if err != nil {
		return record, false, err
	}
	if name != nil {
		record.Name = *name
	} // Native Cql Name imposes no API length check.
	activated, err := userMigrationNullableBool(row["activated"])
	if err != nil {
		return record, false, err
	}
	if activated != nil {
		record.Activated = *activated
	}
	record.Searchable, err = userMigrationNullableBool(row["searchable"])
	if err != nil {
		return record, false, err
	}
	writetime, hasWritetime := int64(0), row["writetime(activated)"] != nil
	if hasWritetime {
		var ok bool
		writetime, ok = cqlInteger(row["writetime(activated)"])
		if !ok {
			return record, false, invalidUserMigrationRow()
		}
		record.CreatedAt = time.Unix(writetime/1000000, (writetime%1000000)*1000).UTC()
	}
	for column, target := range map[string]**int32{"feature_conference_calling": &record.ConferenceCalling, "managed_by": &record.ManagedBy, "status": &record.Status, "supported_protocols": &record.SupportedProtocols} {
		value, err := userMigrationNullableInt32(row[column])
		if err != nil {
			return record, false, err
		}
		if value != nil {
			if column == "managed_by" && (*value < 0 || *value > 1) || column == "status" && (*value < 0 || *value > 4) {
				return record, false, invalidUserMigrationRow()
			}
			if column == "supported_protocols" {
				*value &= 3
			} // Source drops unknown bit flags.
		}
		*target = value
	}
	accent, err := userMigrationNullableInt32(row["accent_id"])
	if err != nil {
		return record, false, err
	}
	if accent != nil {
		record.AccentID = *accent
	}
	userType, err := userMigrationNullableInt32(row["user_type"])
	if err != nil {
		return record, false, err
	}
	if userType != nil {
		if *userType < 0 || *userType > 2 {
			return record, false, invalidUserMigrationRow()
		}
		record.UserType = *userType
	}
	for column, target := range map[string]*pgtype.UUID{"provider": &record.Provider, "service": &record.Service, "team": &record.Team} {
		if row[column] == nil {
			continue
		}
		value, ok := cqlUUID(row[column])
		if !ok || !value.Valid {
			return record, false, invalidUserMigrationRow()
		}
		*target = value
	}
	if row["expires"] != nil {
		value, ok := row["expires"].(time.Time)
		if !ok {
			return record, false, invalidUserMigrationRow()
		}
		value = value.UTC().Truncate(time.Millisecond)
		record.Expires = &value
	}
	record.Country, err = decodeUserMigrationLocale(row["country"], true)
	if err != nil {
		return record, false, err
	}
	record.Language, err = decodeUserMigrationLocale(row["language"], false)
	if err != nil {
		return record, false, err
	}
	for column, target := range map[string]**string{"email": &record.Email, "email_unvalidated": &record.EmailUnvalidated} {
		if row[column] == nil {
			continue
		}
		value, err := userMigrationString(row[column])
		if err != nil {
			return record, false, err
		}
		value, err = canonicalUserMigrationEmail(value)
		if err != nil {
			return record, false, err
		}
		*target = &value
	}
	if row["password"] != nil {
		blob, ok := row["password"].([]byte)
		if !ok {
			return record, false, invalidUserMigrationRow()
		}
		encoded, err := canonicalStoredPassword(blob)
		if err != nil {
			return record, false, invalidUserMigrationRow()
		}
		value := string(encoded)
		record.Password = &value
	}
	record.Picture, err = decodeUserMigrationPicture(row["picture"])
	if err != nil {
		return record, false, err
	}
	return record, name != nil && activated != nil && hasWritetime, nil
}

func userMigrationNullableBool(value any) (*bool, error) {
	if value == nil {
		return nil, nil
	}
	b, ok := value.(bool)
	if !ok {
		return nil, invalidUserMigrationRow()
	}
	return &b, nil
}
