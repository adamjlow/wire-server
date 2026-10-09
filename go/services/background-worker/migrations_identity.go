// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/jackc/pgx/v5/pgtype"
)

func (s *postgresStore) migrationExec(ctx context.Context, sql string, args ...any) error {
	return retryPostgres(ctx, s.Pool.Reset, func() error {
		conn, err := s.acquire(ctx)
		if err != nil {
			return err
		}
		defer conn.Release()
		if _, err := conn.Exec(ctx, sql, args...); err != nil {
			return postgresFailure(err, "migration write failed")
		}
		return nil
	})
}

func (s *Service) migrateBlockList(ctx context.Context, cassandra *gocql.Session, postgres *postgresStore) error {
	page := cassandraMigrationPage(cassandra, `SELECT key FROM blacklist`, "key")
	page = validatedMigrationPage(page, func(row migrationRow) error {
		if _, ok := row["key"].(string); !ok {
			return errors.New("invalid stored block-list key")
		}
		return nil
	})
	return s.migrateRows(ctx, "block_list", page, func(ctx context.Context, row migrationRow) (bool, error) {
		key, ok := row["key"].(string)
		if !ok {
			return false, errors.New("invalid stored block-list key")
		}
		err := postgres.migrationExec(ctx, `INSERT INTO email_blocklist (email) VALUES ($1::text) ON CONFLICT DO NOTHING`, key)
		return err == nil, err
	})
}

type passwordResetMigrationRow struct {
	Key, Code string
	User      pgtype.UUID
	Retries   *int32
	Timeout   *time.Time
	TTL       int32
}

func validBase64URLCharacters(value string) bool {
	// Native AsciiBase64Url validates only its alphabet, not padding position
	// or a decodable base64 encoding. Do not impose a stronger encoding rule.
	for _, c := range value {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '=') {
			return false
		}
	}
	return true
}

func decodePasswordResetMigration(row migrationRow) (passwordResetMigrationRow, error) {
	var result passwordResetMigrationRow
	invalid := errors.New("invalid stored password reset row")
	var ok bool
	result.Key, ok = row["key"].(string)
	if !ok || !validBase64URLCharacters(result.Key) {
		return result, invalid
	}
	result.Code, ok = row["code"].(string)
	if !ok || !validBase64URLCharacters(result.Code) {
		return result, invalid
	}
	result.User, ok = cqlUUID(row["user"])
	if !ok || !result.User.Valid {
		return result, invalid
	}
	ttl, ok := cqlInteger(row["ttl(code)"])
	if !ok || ttl < math.MinInt32 || ttl > math.MaxInt32 {
		return result, invalid
	}
	result.TTL = int32(ttl)
	if row["retries"] != nil {
		n, ok := cqlInteger(row["retries"])
		if !ok || n < math.MinInt32 || n > math.MaxInt32 {
			return result, invalid
		}
		v := int32(n)
		result.Retries = &v
	}
	if row["timeout"] != nil {
		t, ok := row["timeout"].(time.Time)
		if !ok {
			return result, invalid
		}
		result.Timeout = &t
	}
	return result, nil
}

func (s *Service) migratePasswordReset(ctx context.Context, cassandra *gocql.Session, postgres *postgresStore) error {
	page := cassandraMigrationPage(cassandra, `SELECT key,code,user,retries,timeout,ttl(code) FROM password_reset`, "key", "code", "user", "retries", "timeout", "ttl(code)")
	page = validatedMigrationPage(page, func(row migrationRow) error { _, err := decodePasswordResetMigration(row); return err })
	return s.migrateRows(ctx, "password_reset", page, func(ctx context.Context, row migrationRow) (bool, error) {
		return copyPasswordResetRow(ctx, row, postgres.migrationExec)
	})
}

func copyPasswordResetRow(ctx context.Context, row migrationRow, exec func(context.Context, string, ...any) error) (bool, error) {
	value, err := decodePasswordResetMigration(row)
	if err != nil {
		return false, err
	}
	if value.TTL <= 0 {
		return false, nil
	}
	if value.Retries == nil || value.Timeout == nil {
		slog.Warn("legacy password reset row omitted from migration")
		return false, nil
	}
	err = exec(ctx, `INSERT INTO password_reset (key,code,user_id,retries,timeout,expires_at) VALUES ($1::text,$2::text,$3::uuid,$4::int4,$5::timestamptz,now()+make_interval(secs=>$6::int)) ON CONFLICT (key) DO UPDATE SET code=$2::text,user_id=$3::uuid,retries=$4::int4,timeout=$5::timestamptz,expires_at=now()+make_interval(secs=>$6::int)`, value.Key, value.Code, value.User, value.Retries, value.Timeout, value.TTL)
	return err == nil, err
}
