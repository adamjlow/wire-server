// SPDX-License-Identifier: AGPL-3.0-only
//go:build brig_storage_qualification

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

// Read-only witness: observe actual Cassandra bytes, never compute an oracle
// fingerprint from the candidate key algorithm.
func TestOwnedServiceFingerprint(t *testing.T) {
	if os.Getuid() != 65534 || os.Getenv("WIRE_E2E_DISPOSABLE") != "1" {
		t.Fatal("owned nonroot runner required")
	}
	var input struct{ Provider, Service, Output string }
	raw, err := os.ReadFile(os.Getenv("BRIG_SERVICE_FINGERPRINT_INPUT"))
	if err != nil || json.Unmarshal(raw, &input) != nil {
		t.Fatal("synthetic fingerprint input invalid")
	}
	provider, err := gocql.ParseUUID(input.Provider)
	if err != nil {
		t.Fatal(err)
	}
	service, err := gocql.ParseUUID(input.Service)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c, err := LoadConfig(os.Getenv("BRIG_STORE_CONFIG"))
	if err != nil {
		t.Fatal(err)
	}
	session, err := openCassandra(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	var fingerprints [][]byte
	var enabled bool
	if err := session.Query(`SELECT fingerprints,enabled FROM service WHERE provider=? AND id=?`, provider, service).WithContext(ctx).Consistency(gocql.LocalQuorum).Scan(&fingerprints, &enabled); err != nil {
		t.Fatal(err)
	}
	values := []string{}
	for _, value := range fingerprints {
		values = append(values, hex.EncodeToString(value))
	}
	encoded, err := json.Marshal(map[string]any{"fingerprints": values, "enabled": enabled})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(input.Output, append(encoded, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}
