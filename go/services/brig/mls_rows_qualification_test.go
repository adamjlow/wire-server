//go:build brig_storage_qualification

// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

// Synthetic supplemental fixture writer/reader. Expected native HTTP and row
// assertions live in the observer; this helper never calls candidate decoders.
func TestOwnedMLSRowFixture(t *testing.T) {
	if os.Getuid() != 65534 || os.Getenv("WIRE_E2E_DISPOSABLE") != "1" {
		t.Fatal("owned nonroot runner required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c, err := LoadConfig(os.Getenv("BRIG_STORE_CONFIG"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := openCassandra(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var input struct {
		User, Client, Action, Output string
		Rows                         []struct{ Ref, Data string }
	}
	raw, err := os.ReadFile(os.Getenv("BRIG_MLS_ROWS_INPUT"))
	if err != nil {
		t.Fatal(err)
	}
	if json.Unmarshal(raw, &input) != nil {
		t.Fatal("invalid fixture manifest")
	}
	user, err := gocql.ParseUUID(input.User)
	if err != nil {
		t.Fatal(err)
	}
	client, err := parseClientID(input.Client)
	if err != nil {
		t.Fatal(err)
	}
	if input.Action == "stage" {
		if err := store.Query(`DELETE FROM mls_key_packages WHERE user=? AND client=? AND cipher_suite=?`, user, client, int32(1)).WithContext(ctx).Consistency(gocql.LocalQuorum).Exec(); err != nil {
			t.Fatal(err)
		}
		for _, row := range input.Rows {
			ref, err := hex.DecodeString(row.Ref)
			if err != nil {
				t.Fatal(err)
			}
			data, err := hex.DecodeString(row.Data)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Query(`INSERT INTO mls_key_packages (user,client,cipher_suite,ref,data) VALUES (?,?,?,?,?)`, user, client, int32(1), ref, data).WithContext(ctx).Consistency(gocql.LocalQuorum).Exec(); err != nil {
				t.Fatal(err)
			}
		}
	} else if input.Action != "read" {
		t.Fatal("invalid fixture operation")
	}
	iter := store.Query(`SELECT ref,data FROM mls_key_packages WHERE user=? AND client=? AND cipher_suite=?`, user, client, int32(1)).WithContext(ctx).Consistency(gocql.LocalQuorum).Iter()
	rows := []map[string]string{}
	var ref, data []byte
	for iter.Scan(&ref, &data) {
		rows = append(rows, map[string]string{"ref": hex.EncodeToString(ref), "data": hex.EncodeToString(data)})
	}
	if err := iter.Close(); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(input.Output, encoded, 0600); err != nil {
		t.Fatal(err)
	}
}
