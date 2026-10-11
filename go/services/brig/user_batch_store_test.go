// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"testing"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func TestMigratingBatchUsesRequestedOrderAndPostgresTombstones(t *testing.T) {
	ids := make([]gocql.UUID, 3)
	for i := range ids {
		ids[i][15] = byte(i + 1)
	}
	deleted := int32(2)
	cass := []storedUser{{ID: ids[0], Name: "stale Cassandra account"}, {ID: ids[1], Name: "Cassandra only"}}
	pg := []storedUser{{ID: ids[0], Name: "default", Status: &deleted}}
	result := mergeMigratingUsers([]gocql.UUID{ids[1], ids[0], ids[2], ids[1]}, cass, pg)
	if len(result) != 3 || result[0].ID != ids[1] || result[1].ID != ids[0] || result[1].Status == nil || *result[1].Status != 2 || result[1].Name != "default" || result[2].ID != ids[1] {
		t.Fatal("migration lookup reordered/deduplicated requests or resurrected deleted identity")
	}
	if result := mergeMigratingUsers(nil, cass, pg); result == nil || len(result) != 0 {
		t.Fatal("empty batch must be empty slice")
	}
}
