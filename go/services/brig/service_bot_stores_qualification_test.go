//go:build brig_storage_qualification

// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"reflect"
	"testing"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func qualifyServiceBotStores(t *testing.T, ctx context.Context, stores map[string]userStore, record func(gocql.UUID) storedUser) {
	t.Helper()
	newID := func() gocql.UUID {
		id, err := gocql.RandomUUID()
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	provider, service, conversation, team := newID(), newID(), newID(), newID()
	ids := map[string][]gocql.UUID{}
	for _, mode := range []string{"cassandra", "postgresql"} {
		for i := 0; i < 101; i++ {
			id := newID()
			u := record(id)
			u.Provider = &provider
			u.Service = &service
			if err := stores[mode].create(ctx, u, &conversation, &team); err != nil {
				t.Fatal("bot fixture", mode, err)
			}
			ids[mode] = append(ids[mode], id)
		}
	}
	for _, test := range []struct {
		mode  string
		count int
		sizes []int
	}{{"cassandra", 101, []int{100, 1}}, {"postgresql", 101, []int{100, 1}}, {"migration-to-postgresql", 202, []int{100, 101, 1}}} {
		t.Run("service-bots-"+test.mode, func(t *testing.T) {
			seen := map[gocql.UUID]bool{}
			var sizes []int
			err := stores[test.mode].serviceBotPages(ctx, provider, service, func(bots []deletionServiceBot) error {
				sizes = append(sizes, len(bots))
				for _, bot := range bots {
					if seen[bot.user] || bot.conversation != conversation {
						t.Error("duplicate or wrong bot conversation")
					}
					seen[bot.user] = true
				}
				return nil
			})
			if err != nil || len(seen) != test.count || !reflect.DeepEqual(sizes, test.sizes) {
				t.Fatalf("native pages count=%d sizes=%v err=%v", len(seen), sizes, err)
			}
		})
	}
	// Delete during pagination: opaque Cassandra paging and PostgreSQL keysets
	// must remain usable after each visited page's association rows disappear.
	visited := 0
	mixed := stores["migration-to-postgresql"]
	if err := mixed.serviceBotPages(ctx, provider, service, func(bots []deletionServiceBot) error {
		for _, bot := range bots {
			if err := mixed.deleteServiceBot(ctx, provider, service, bot.user); err != nil {
				return err
			}
			visited++
		}
		return nil
	}); err != nil || visited != 202 {
		t.Fatal("mixed page cleanup", visited, err)
	}
	remaining := 0
	if err := mixed.serviceBotPages(ctx, provider, service, func(bots []deletionServiceBot) error { remaining += len(bots); return nil }); err != nil || remaining != 0 {
		t.Fatal("bot associations remain", remaining, err)
	}
	var count int
	if err := stores["cassandra"].cassandra.Query(`SELECT count(*) FROM service_team WHERE provider=? AND service=? AND team=?`, provider, service, team).WithContext(ctx).Consistency(gocql.LocalQuorum).Scan(&count); err != nil || count != 0 {
		t.Fatal("service team associations remain", err)
	}
	for _, mode := range []string{"cassandra", "postgresql"} {
		id := ids[mode][0]
		if err := stores[mode].deleteServiceBot(ctx, provider, service, id); err != nil {
			t.Fatal("association replay", mode, err)
		}
		u, err := stores[mode].get(ctx, id)
		if err != nil || u == nil || u.Provider == nil || *u.Provider != provider || u.Service == nil || *u.Service != service || u.Status == nil || *u.Status != 0 {
			t.Fatal("association cleanup changed account", mode, err)
		}
	}
	// The native missing-service partition is an empty page, not an error.
	if err := mixed.serviceBotPages(ctx, provider, newID(), func(bots []deletionServiceBot) error {
		if len(bots) != 0 {
			t.Error("foreign service selected")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
