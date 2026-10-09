// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type observedScheduledSchema struct {
	ledger  map[string]string
	lookups []string
	writes  []string
	args    [][]any
	failure error
}
type scheduledSchemaRow struct {
	checksum string
	err      error
}

func (r scheduledSchemaRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*dest[0].(*string) = r.checksum
	return nil
}
func (s *observedScheduledSchema) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	name := args[0].(string)
	s.lookups = append(s.lookups, name)
	checksum, found := s.ledger[name]
	if !found {
		return scheduledSchemaRow{err: pgx.ErrNoRows}
	}
	return scheduledSchemaRow{checksum: checksum}
}
func (s *observedScheduledSchema) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	s.writes = append(s.writes, sql)
	s.args = append(s.args, args)
	if s.failure != nil {
		return pgconn.CommandTag{}, s.failure
	}
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

func TestScheduledSchemaRetainsNativeBodiesNamesOrderAndDefaults(t *testing.T) {
	blob, _ := scheduledSchemaFiles.ReadFile("arbiter_sql/migrations/manifest.json")
	var manifest struct {
		SourceFiles map[string]string `json:"source_files"`
	}
	if json.Unmarshal(blob, &manifest) != nil || len(manifest.SourceFiles) != 44 {
		t.Fatal("native SQL source identities missing")
	}
	for name, want := range manifest.SourceFiles {
		body, err := scheduledSchemaFiles.ReadFile("arbiter_sql/migrations/" + name)
		hash := sha256.Sum256(body)
		if err != nil || hex.EncodeToString(hash[:]) != want {
			t.Fatal("native SQL body changed")
		}
	}
	scripts, err := scheduledSchemaScripts()
	if err != nil || len(scripts) != 76 || scripts[0].Name != "create-cron-schedules" || scripts[12].Name != "meetings-create-table" || scripts[44].Name != "conversations-create-table" || scripts[75].Name != "conversations-create-notify-trigger" {
		t.Fatal("native schema/registry migration order changed")
	}
	seen := make(map[string]bool)
	for _, script := range scripts {
		if seen[script.Name] || strings.Contains(script.SQL, "golden_jobs") || strings.Contains(script.Name, "event-streaming") || strings.Contains(script.Name, "bucket-trigger") || strings.Contains(script.Name, "concurrency-trigger") {
			t.Fatal("duplicate/foreign queue or disabled native feature migrated")
		}
		seen[script.Name] = true
	}
	// Independent Python hashlib MD5/base64 over pinned original golden bodies,
	// rendering the fixed meetings queue. Native library uses this encoding,
	// rather than hexadecimal MD5, in its existing varchar checksum ledger.
	if scripts[0].Checksum != "Q3eoAhi1FzsPWGoHnTTViQ==" || scripts[12].Checksum != "1Dm+3Mmd/miOnUtRNr6Zsg==" {
		t.Fatal("native migration checksum representation changed")
	}
}

func TestScheduledSchemaExistingLedgerIsValidatedWithoutDDLAndMismatchStops(t *testing.T) {
	scripts := []scheduledSchemaScript{{Name: "first", SQL: "synthetic first DDL", Checksum: "native-first"}, {Name: "second", SQL: "synthetic second DDL", Checksum: "native-second"}}
	tx := &observedScheduledSchema{ledger: map[string]string{"first": "native-first", "second": "native-second"}}
	if err := applyScheduledSchema(t.Context(), tx, scripts); err != nil || len(tx.writes) != 0 || !reflect.DeepEqual(tx.lookups, []string{"first", "second"}) {
		t.Fatal("existing native schema reran migrations")
	}
	tx = &observedScheduledSchema{ledger: map[string]string{"first": "modified"}}
	var mismatch *scheduledSchemaMismatch
	if err := applyScheduledSchema(t.Context(), tx, scripts); !errors.As(err, &mismatch) || len(tx.writes) != 0 || len(tx.lookups) != 1 {
		t.Fatal("checksum mismatch ran later scripts or silently overwrote ledger")
	}
	tx = &observedScheduledSchema{ledger: map[string]string{"second": "modified"}}
	if err := applyScheduledSchema(t.Context(), tx, scripts); !errors.As(err, &mismatch) || len(tx.writes) != 2 || tx.writes[0] != scripts[0].SQL {
		t.Fatal("native mismatch did not preserve earlier applied script order")
	}
}

func TestScheduledSchemaNewScriptBindsLedgerAndFailedDDLDoesNotRecordSuccess(t *testing.T) {
	script := scheduledSchemaScript{Name: "first", SQL: "synthetic DDL", Checksum: "native-checksum"}
	tx := &observedScheduledSchema{}
	if err := applyScheduledSchema(t.Context(), tx, []scheduledSchemaScript{script}); err != nil || len(tx.writes) != 2 || !reflect.DeepEqual(tx.args[1], []any{script.Name, script.Checksum}) {
		t.Fatal("native script/checksum ledger order or parameter binding changed")
	}
	tx = &observedScheduledSchema{failure: errors.New("synthetic confidential SQL diagnostic")}
	if err := applyScheduledSchema(t.Context(), tx, []scheduledSchemaScript{script}); err == nil || strings.Contains(err.Error(), "confidential") || len(tx.writes) != 1 {
		t.Fatal("failed DDL recorded success or leaked database diagnostics")
	}
}
