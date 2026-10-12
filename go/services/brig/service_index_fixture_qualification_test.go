// SPDX-License-Identifier: AGPL-3.0-only
//go:build brig_storage_qualification

package brig

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/apache/cassandra-gocql-driver/v2"
)

// Observe exact index rows from native and Go updates, without changing them.
func TestOwnedServiceIndexes(t *testing.T) {
	if os.Getuid() != 65534 || os.Getenv("WIRE_E2E_DISPOSABLE") != "1" {
		t.Fatal("owned nonroot runner required")
	}
	var input struct {
		Provider, Service, Output string
		Names                     []string
		Masks                     []int64
	}
	raw, err := os.ReadFile(os.Getenv("BRIG_SERVICE_INDEX_INPUT"))
	if err != nil || json.Unmarshal(raw, &input) != nil {
		t.Fatal("synthetic index input invalid")
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
	_ = provider
	rows := []map[string]any{}
	for _, name := range input.Names {
		ch, _ := utf8.DecodeRuneInString(name)
		lower := deletionServiceLower(name)
		prefix := deletionServiceLower(string(ch))
		var count int
		if err := session.Query(`SELECT count(*) FROM service_prefix WHERE prefix=? AND name=? AND service=?`, prefix, lower, service).WithContext(ctx).Consistency(gocql.LocalQuorum).Scan(&count); err != nil {
			t.Fatal(err)
		}
		tags := []int64{}
		for _, mask := range input.Masks {
			var value int
			if err := session.Query(`SELECT count(*) FROM service_tag WHERE bucket=? AND tag=? AND name=? AND service=?`, int32(201608), mask, lower, service).WithContext(ctx).Consistency(gocql.LocalQuorum).Scan(&value); err != nil {
				t.Fatal(err)
			}
			if value != 0 {
				tags = append(tags, mask)
			}
		}
		rows = append(rows, map[string]any{"name": lower, "prefix_count": count, "tag_masks": tags})
	}
	encoded, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(input.Output, append(encoded, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}
