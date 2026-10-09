// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"crypto/md5" // Native migration ledger checksum; not authentication.
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Native Arbiter SQL bodies remain byte-for-byte intact; render only its fixed
// queue identifier before computing the native Base64(MD5(body)) checksum.
//
//go:embed arbiter_sql/migrations/*
var scheduledSchemaFiles embed.FS

type scheduledSchemaScript struct{ Name, SQL, Checksum string }

func scheduledSchemaScripts() ([]scheduledSchemaScript, error) {
	var manifest struct{ Schema, Queue []string }
	blob, err := scheduledSchemaFiles.ReadFile("arbiter_sql/migrations/manifest.json")
	if err != nil || json.Unmarshal(blob, &manifest) != nil {
		return nil, errors.New("invalid embedded scheduled migration manifest")
	}
	var scripts []scheduledSchemaScript
	add := func(name, file, queue string) error {
		body, err := scheduledSchemaFiles.ReadFile("arbiter_sql/migrations/" + file + ".sql")
		if err != nil {
			return errors.New("missing embedded scheduled migration")
		}
		sql := string(body)
		if queue != "" {
			sql = strings.ReplaceAll(sql, "golden_jobs", queue)
		}
		digest := md5.Sum([]byte(sql))
		scripts = append(scripts, scheduledSchemaScript{Name: name, SQL: sql, Checksum: base64.StdEncoding.EncodeToString(digest[:])})
		return nil
	}
	for _, name := range manifest.Schema {
		if err := add(name, name, ""); err != nil {
			return nil, err
		}
	}
	// Wire.API.Jobs.JobRegistry has no rate-limit/concurrency policies. Native
	// defaults enable notify triggers and disable event-streaming triggers.
	for _, queue := range []string{"meetings", "conversations"} {
		for _, name := range manifest.Queue {
			if err := add(queue+"-"+name, "golden_jobs-"+name, queue); err != nil {
				return nil, err
			}
		}
	}
	return scripts, nil
}

type scheduledSchemaSQL interface {
	userMigrationSQL
	QueryRow(context.Context, string, ...any) pgx.Row
}

type scheduledSchemaMismatch struct{}

func (*scheduledSchemaMismatch) Error() string { return "scheduled migration checksum mismatch" }

func applyScheduledSchema(ctx context.Context, tx scheduledSchemaSQL, scripts []scheduledSchemaScript) error {
	for _, script := range scripts {
		var checksum string
		err := tx.QueryRow(ctx, `SELECT checksum FROM arbiter.schema_migrations WHERE filename=$1::text LIMIT 1`, script.Name).Scan(&checksum)
		switch {
		case err == nil:
			if checksum != script.Checksum {
				return &scheduledSchemaMismatch{}
			}
		case errors.Is(err, pgx.ErrNoRows):
			// Bodies contain multiple native DDL statements and no input. With no arguments pgx uses
			// simple protocol; row values remain bound elsewhere.
			if _, err := tx.Exec(ctx, script.SQL); err != nil {
				return postgresFailure(err, "scheduled schema migration failed")
			}
			if _, err := tx.Exec(ctx, `INSERT INTO arbiter.schema_migrations (filename,checksum) VALUES ($1::text,$2::text)`, script.Name, script.Checksum); err != nil {
				return postgresFailure(err, "scheduled schema migration ledger failed")
			}
		default:
			return postgresFailure(err, "scheduled schema migration lookup failed")
		}
	}
	return nil
}

const scheduledSchemaLedger = `CREATE TABLE IF NOT EXISTS arbiter.schema_migrations (filename varchar(512) NOT NULL,checksum varchar(32) NOT NULL,executed_at timestamp without time zone NOT NULL DEFAULT now())`
const scheduledAdminlessTeamIndex = `CREATE INDEX IF NOT EXISTS "conversations_adminless_team_id_idx" ON "arbiter"."conversations" ((payload #>> '{data,team_id}')) WHERE claimed_by IS NULL AND payload->>'type' IN ('adminless_setup','adminless_deletion','adminless_reminder')`

func runScheduledSchema(ctx context.Context, c PostgresPoolConfig, g GalleyConfig) error {
	config, err := postgresConfig(c, g)
	if err != nil {
		return err
	}
	// Native migrations use dedicated connections, independently of Hasql
	// pool acquisition/reset behavior. Keep the session lock for the whole run.
	lock, err := pgx.ConnectConfig(ctx, config.ConnConfig.Copy())
	if err != nil {
		return errors.New("scheduled migration lock connection failed")
	}
	defer func() { _ = lock.Close(context.Background()) }()
	key := textMigrationLockKey("wire-server:arbiter-migrations:arbiter")
	lockCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	for {
		var acquired bool
		if err := lock.QueryRow(lockCtx, `SELECT pg_try_advisory_lock($1::bigint)`, key).Scan(&acquired); err != nil {
			return postgresFailure(err, "scheduled migration lock query failed")
		}
		if acquired {
			break
		}
		if !wait(lockCtx, time.Second) {
			return errors.New("scheduled migration lock wait timed out")
		}
	}
	defer func() {
		releaseCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_, _ = lock.Exec(releaseCtx, `SELECT pg_advisory_unlock($1::bigint)`, key)
	}()
	conn, err := pgx.ConnectConfig(ctx, config.ConnConfig.Copy())
	if err != nil {
		return errors.New("scheduled migration connection failed")
	}
	defer func() { _ = conn.Close(context.Background()) }()
	if _, err := conn.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS "arbiter"`); err != nil {
		// Native tolerates insufficient CREATE privilege if the schema is
		// already present, allowing deployments with preprovisioned schemas.
		var exists bool
		if conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname='arbiter')`).Scan(&exists) != nil || !exists {
			return postgresFailure(err, "scheduled schema creation failed")
		}
	}
	if _, err := conn.Exec(ctx, scheduledSchemaLedger); err != nil {
		return postgresFailure(err, "scheduled migration initialization failed")
	}
	scripts, err := scheduledSchemaScripts()
	if err != nil {
		return err
	}
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return postgresFailure(err, "scheduled migration transaction failed")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	err = applyScheduledSchema(ctx, tx, scripts)
	var mismatch *scheduledSchemaMismatch
	if err != nil && !errors.As(err, &mismatch) {
		return err
	}
	// Native returns MigrationError rather than throwing on checksum mismatch:
	// preceding successful scripts are committed. SQL exceptions roll back.
	if commitErr := tx.Commit(ctx); commitErr != nil {
		return postgresFailure(commitErr, "scheduled migration commit failed")
	}
	if err != nil {
		return err
	}
	var durability string
	if err := conn.QueryRow(ctx, `SELECT relpersistence::text FROM pg_class WHERE oid='arbiter.arbiter_rate_limits'::regclass`).Scan(&durability); err != nil {
		return postgresFailure(err, "scheduled rate limit durability lookup failed")
	}
	if durability != "u" {
		if _, err := conn.Exec(ctx, `ALTER TABLE "arbiter".arbiter_rate_limits SET UNLOGGED`); err != nil {
			return postgresFailure(err, "scheduled rate limit durability reconciliation failed")
		}
	}
	if _, err := lock.Exec(ctx, scheduledAdminlessTeamIndex); err != nil {
		return postgresFailure(err, "scheduled adminless index creation failed")
	}
	return nil
}
