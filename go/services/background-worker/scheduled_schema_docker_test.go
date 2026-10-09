//go:build schemacheck

// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// Explicit Docker-only qualifier, excluded from ordinary unit/race checks.
// Fixed isolated fixture target; never accept a user-provided database URL.
func TestScheduledSchemaDockerStorage(t *testing.T) {
	if os.Getenv("WIRE_SCHEMA_CHECK_SYNTHETIC") != "1" {
		t.Fatal("schema qualifier requires the owned synthetic Docker fixture")
	}
	ctx, stop := context.WithTimeout(t.Context(), time.Minute)
	defer stop()
	g := GalleyConfig{Postgres: map[string]string{"host": "postgres", "port": "5432", "user": "postgres", "password": "synthetic-schema-only", "dbname": "backendA", "sslmode": "disable"}}
	config := PostgresPoolConfig{Size: 2, Acquisition: Duration(5 * time.Second), Idleness: Duration(time.Minute)}
	if err := runScheduledSchema(ctx, config, g); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, "host=postgres port=5432 user=postgres password=synthetic-schema-only dbname=backendA sslmode=disable")
	if err != nil {
		t.Fatal("fixture database unavailable")
	}
	defer func() { _ = conn.Close(context.Background()) }()
	var actualJSON []byte
	if err := conn.QueryRow(ctx, `SELECT coalesce(json_agg(row_to_json(c) ORDER BY table_name,ordinal_position),'[]') FROM information_schema.columns c WHERE table_schema='arbiter'`).Scan(&actualJSON); err != nil {
		t.Fatal("schema inspection failed")
	}
	expectedJSON, err := os.ReadFile("/reference/columns.json")
	if err != nil {
		t.Fatal("native captured schema columns unavailable")
	}
	var actual, expected []map[string]any
	if json.Unmarshal(actualJSON, &actual) != nil || json.Unmarshal(expectedJSON, &expected) != nil {
		t.Fatal("invalid schema column capture")
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("native observed schema columns differ: got%d expected%d", len(actual), len(expected))
	}
	var count int
	if conn.QueryRow(ctx, `SELECT count(*) FROM arbiter.schema_migrations`).Scan(&count) != nil || count != 76 {
		t.Fatal("native migration ledger incomplete")
	}
	// Native group triggers must keep queue/group state useful to either worker.
	if _, err := conn.Exec(ctx, `INSERT INTO arbiter.meetings (payload,group_key) VALUES ('{"type":"meetings_cleanup","data":{}}','synthetic-group')`); err != nil {
		t.Fatal("native queue/group trigger insert failed")
	}
	if err := runScheduledSchema(ctx, config, g); err != nil {
		t.Fatal("existing scheduled schema did not resume")
	}
	if conn.QueryRow(ctx, `SELECT count(*) FROM arbiter.meetings WHERE group_key='synthetic-group'`).Scan(&count) != nil || count != 1 {
		t.Fatal("bootstrap rerun lost queued workload")
	}
	// Reject a corrupt existing checksum while preserving the native commit of
	// an earlier missing entry. No fixture/expectation is modified by this probe.
	if _, err := conn.Exec(ctx, `DELETE FROM arbiter.schema_migrations WHERE filename='create-cron-schedules'; UPDATE arbiter.schema_migrations SET checksum='synthetic-corrupt-checksum' WHERE filename='conversations-create-notify-trigger'`); err != nil {
		t.Fatal("fixture mismatch setup failed")
	}
	var mismatch *scheduledSchemaMismatch
	if err := runScheduledSchema(ctx, config, g); !errors.As(err, &mismatch) {
		t.Fatal("native checksum mismatch did not reject bootstrap")
	}
	if conn.QueryRow(ctx, `SELECT count(*) FROM arbiter.schema_migrations WHERE filename='create-cron-schedules'`).Scan(&count) != nil || count != 1 {
		t.Fatal("native partial migration commit behavior changed")
	}
	t.Logf("native schema columns%d; ledger76; queued workload preserved on rerun; mismatch stops with native preceding commit", len(actual))
}
