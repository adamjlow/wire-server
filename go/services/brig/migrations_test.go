// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

// The schema inputs are native-owned contracts. A changed, missing or extra
// script must be explicitly reconciled with upstream, not silently rewritten.
func TestNativeMigrationInputs(t *testing.T) {
	data, err := os.ReadFile("qualification/migration-inputs.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Files map[string]string `json:"files_sha256"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	scripts, err := postgresScripts()
	if err != nil {
		t.Fatal(err)
	}
	if len(scripts) != len(manifest.Files) || len(scripts) != 32 {
		t.Fatal("native migration set changed")
	}
	concurrent := 0
	for i, script := range scripts {
		if i > 0 && scripts[i-1].Name >= script.Name {
			t.Fatal("native filename order changed")
		}
		digest := sha256.Sum256([]byte(script.SQL))
		if hex.EncodeToString(digest[:]) != manifest.Files[script.Name] {
			t.Fatalf("native SQL bytes changed: %s", script.Name)
		}
		if script.Concurrent {
			concurrent++
		}
	}
	if concurrent != 3 {
		t.Fatal("native nontransactional index set changed")
	}
	// Independent native hasql-migration ledger value for the first raw script.
	if scripts[0].Checksum != "E2arIjoqD7wKiN0aehpK8A==" {
		t.Fatalf("first native checksum changed: %s", scripts[0].Checksum)
	}
}
