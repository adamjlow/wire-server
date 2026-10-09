// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"errors"
	"math"
	"unicode/utf8"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/jackc/pgx/v5/pgtype"
)

type codeMigrationRow struct {
	Key, Value   string
	Conversation pgtype.UUID
	Password     []byte
	TTL          int32
}

func decodeCodeMigration(row migrationRow) (codeMigrationRow, error) {
	var result codeMigrationRow
	invalid := errors.New("invalid stored conversation code row")
	var ok bool
	result.Key, ok = row["key"].(string)
	if !ok || utf8.RuneCountInString(result.Key) != 20 {
		return result, invalid
	}
	result.Value, ok = row["value"].(string)
	if !ok || utf8.RuneCountInString(result.Value) < 6 || utf8.RuneCountInString(result.Value) > 20 {
		return result, invalid
	}
	// Native Cql AsciiText bypasses API alphabet validation; Range still
	// checks lengths. Preserve stored text rather than regenerating secrets.
	result.Conversation, ok = cqlUUID(row["conversation"])
	if !ok || !result.Conversation.Valid {
		return result, invalid
	}
	ttl, ok := cqlInteger(row["ttl(value)"])
	if !ok || ttl < math.MinInt32 || ttl > math.MaxInt32 {
		return result, invalid
	}
	result.TTL = int32(ttl)
	if row["password"] != nil {
		blob, ok := row["password"].([]byte)
		if !ok {
			return result, invalid
		}
		var err error
		result.Password, err = canonicalStoredPassword(blob)
		if err != nil {
			return result, invalid
		}
	}
	return result, nil
}

func copyCodeRow(ctx context.Context, row migrationRow, exec func(context.Context, string, ...any) error) (bool, error) {
	value, err := decodeCodeMigration(row)
	if err != nil {
		return false, err
	}
	if value.TTL <= 0 {
		return false, nil
	}
	err = exec(ctx, `INSERT INTO conversation_codes (key,conversation,password,value,expires_at,target) VALUES ($1::text,$2::uuid,$3::bytea,$4::text,now()+make_interval(secs=>$5::int),$6::text) ON CONFLICT (key) DO UPDATE SET conversation=$2::uuid,password=$3::bytea,value=$4::text,expires_at=now()+make_interval(secs=>$5::int),target=$6::text`, value.Key, value.Conversation, value.Password, value.Value, value.TTL, "conv")
	return err == nil, err
}

func (s *Service) migrateCodes(ctx context.Context, cassandra *gocql.Session, postgres *postgresStore) error {
	page := cassandraMigrationPage(cassandra, `SELECT key,value,ttl(value),conversation,password FROM conversation_codes`, "key", "value", "ttl(value)", "conversation", "password")
	page = validatedMigrationPage(page, func(row migrationRow) error { _, err := decodeCodeMigration(row); return err })
	return s.migrateRows(ctx, "conv_codes", page, func(ctx context.Context, row migrationRow) (bool, error) {
		return copyCodeRow(ctx, row, postgres.migrationExec)
	})
}
