// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"crypto/md5" // Native migration ledger identity, not authentication.
	"embed"
	"encoding/base64"
	"errors"
	"fmt"
	"math/bits"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Source script bytes are copied intact; no query or migration rewriting.
//
//go:embed postgres-migrations/*.sql
var migrationFiles embed.FS

type migrationScript struct {
	Name, SQL, Checksum string
	Concurrent          bool
}

func postgresScripts() ([]migrationScript, error) {
	entries, err := migrationFiles.ReadDir("postgres-migrations")
	if err != nil {
		return nil, errors.New("missing PostgreSQL migrations")
	}
	scripts := make([]migrationScript, 0, len(entries))
	for _, entry := range entries {
		body, err := migrationFiles.ReadFile("postgres-migrations/" + entry.Name())
		if err != nil || !utf8.Valid(body) {
			return nil, errors.New("invalid PostgreSQL migration")
		}
		digest := md5.Sum(body)
		script := migrationScript{Name: entry.Name(), SQL: string(body), Checksum: base64.StdEncoding.EncodeToString(digest[:])}
		switch script.Name {
		case "20260428072649-create-conv-parent-index.sql", "20260708090000-meetings-recurrence-eff-end-index.sql", "20260708100000-meetings-end-time-nonrecurring-index.sql":
			script.Concurrent = true
		}
		scripts = append(scripts, script)
	}
	return scripts, nil
}

// Native ScriptName is String ([Char]), not Text. hashable-1.5.1.0
// hashes characters then the list length from its default64-bit salt. Its Text
// instance instead uses XXH3, so the background-worker Text lock is unsuitable.
func migrationLockKey(name string) int64 {
	h := uint64(14695981039346656037)
	mix := func(a, b uint64) uint64 {
		hi, lo := bits.Mul64(a, b)
		z := bits.ReverseBytes64(b) + a + (hi ^ lo)
		z = (z ^ (z >> 33)) * 0xff51afd7ed558ccd
		z = (z ^ (z >> 33)) * 0xc4ceb9fe1a85ec53
		return z ^ (z >> 33)
	}
	var length uint64
	for _, r := range name {
		h = mix(h, uint64(r))
		length++
	}
	return int64(mix(h, length))
}

type migrationSQL interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

const migrationLedger = `CREATE TABLE IF NOT EXISTS schema_migrations (filename varchar(512) NOT NULL, checksum varchar(32) NOT NULL, executed_at timestamp without time zone NOT NULL DEFAULT now())`

func applyMigration(ctx context.Context, db migrationSQL, script migrationScript) error {
	var checksum string
	err := db.QueryRow(ctx, `SELECT checksum FROM schema_migrations WHERE filename=$1::varchar LIMIT 1`, script.Name).Scan(&checksum)
	if err == nil {
		if checksum != script.Checksum {
			return fmt.Errorf("PostgreSQL migration checksum mismatch: %s", script.Name)
		}
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	// Embedded scripts contain no request input. No arguments uses pgx simple
	// protocol, preserving multiple native statements, including CONCURRENTLY.
	if _, err = db.Exec(ctx, script.SQL); err != nil {
		return err
	}
	_, err = db.Exec(ctx, `INSERT INTO schema_migrations (filename,checksum) VALUES ($1::text,$2::text)`, script.Name, script.Checksum)
	return err
}

func migrationTransaction(ctx context.Context, conn *pgx.Conn, action func(pgx.Tx) error) error {
	for {
		tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable, AccessMode: pgx.ReadWrite})
		if err != nil {
			return err
		}
		err = action(tx)
		if err == nil {
			err = tx.Commit(ctx)
		} else {
			_ = tx.Rollback(ctx)
		}
		var database *pgconn.PgError
		if errors.As(err, &database) && database.Code == "40001" {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			continue
		}
		return err
	}
}

func runPostgresMigrations(ctx context.Context, pool *pgxpool.Pool, timeout time.Duration) error {
	scripts, err := postgresScripts()
	if err != nil {
		return err
	}
	acquire, cancel := context.WithTimeout(ctx, timeout)
	lease, err := pool.Acquire(acquire)
	cancel()
	if err != nil {
		return errors.New("PostgreSQL migration connection failed")
	}
	conn := lease.Conn()
	// Drop a failing session instead of returning an advisory lock or aborted
	// transaction to the pool. Native Hasql drops failed migration sessions.
	defer func() {
		if err != nil {
			closeCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			_ = conn.Close(closeCtx)
		}
		lease.Release()
	}()
	err = migrationTransaction(ctx, conn, func(tx pgx.Tx) error { _, e := tx.Exec(ctx, migrationLedger); return e })
	for _, script := range scripts {
		if err != nil {
			break
		}
		if !script.Concurrent {
			err = migrationTransaction(ctx, conn, func(tx pgx.Tx) error { return applyMigration(ctx, tx, script) })
			continue
		}
		key := migrationLockKey(script.Name)
		for {
			var acquired bool
			if err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1::bigint)`, key).Scan(&acquired); err != nil || acquired {
				break
			}
			timer := time.NewTimer(time.Second)
			select {
			case <-ctx.Done():
				err = ctx.Err()
				if !timer.Stop() {
					<-timer.C
				}
			case <-timer.C:
			}
			if err != nil {
				break
			}
		}
		if err != nil {
			break
		}
		err = applyMigration(ctx, conn, script)
		if err == nil {
			_, err = conn.Exec(ctx, `SELECT pg_advisory_unlock($1::bigint)`, key)
		}
	}
	if err != nil {
		// Preserve mismatch names, never SQL/connection diagnostics or secrets.
		if strings.HasPrefix(err.Error(), "PostgreSQL migration checksum mismatch:") {
			return err
		}
		return errors.New("PostgreSQL migration failed")
	}
	return nil
}

// MigratePostgres is the native CLI operation. Reset runs only when explicitly
// requested, as separate autocommit statements, matching the native utility.
func MigratePostgres(ctx context.Context, c Config, dbname string, reset bool) error {
	if dbname != "" {
		params := make(map[string]string, len(c.Postgres))
		for k, v := range c.Postgres {
			params[k] = v
		}
		params["dbname"] = dbname
		c.Postgres = params
	}
	pool, err := openPostgres(ctx, c)
	if err != nil {
		return err
	}
	defer pool.Close()
	if reset {
		lease, err := pool.Acquire(ctx)
		if err != nil {
			return errors.New("PostgreSQL reset connection failed")
		}
		_, err = lease.Exec(ctx, `DROP SCHEMA IF EXISTS public CASCADE`)
		if err == nil {
			_, err = lease.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS public`)
		}
		lease.Release()
		if err != nil {
			return errors.New("PostgreSQL reset failed")
		}
	}
	return runPostgresMigrations(ctx, pool, time.Duration(c.PostgresPool.Acquisition))
}
