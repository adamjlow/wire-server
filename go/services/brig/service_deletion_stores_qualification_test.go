//go:build brig_storage_qualification

// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"testing"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func qualifyServiceDeletionStores(t *testing.T, ctx context.Context, cassandra *gocql.Session) {
	t.Helper()
	newID := func() gocql.UUID {
		id, err := gocql.RandomUUID()
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	provider, service, other, teamA, teamB := newID(), newID(), newID(), newID(), newID()
	s := serviceDeletionStore{cassandra}
	r := syntheticDeletionServiceRow()
	name := "İΣynthetic"
	r.name = &name
	r.tags = []int64{1, 2, 4}
	write := func(row deletionServiceRow) {
		t.Helper()
		if err := cassandra.Query(`INSERT INTO service (provider,id,name,summary,descr,base_url,auth_tokens,pubkeys,assets,tags,enabled) VALUES (?,?,?,?,?,?,?,?,?,?,?)`, provider, service, row.name, row.summary, row.description, row.baseURL, row.tokens, row.keys, row.assets, row.tags, row.enabled).WithContext(ctx).Consistency(gocql.LocalQuorum).Exec(); err != nil {
			t.Fatal("native service fixture", err)
		}
	}
	write(r)
	got, err := s.lookup(ctx, provider, service)
	if err != nil || got == nil || got.name != name || len(got.tags) != 3 {
		t.Fatal("full native service row lookup", err)
	}
	if got, err := s.lookup(ctx, provider, other); err != nil || got != nil {
		t.Fatal("missing service", err)
	}
	for _, change := range []func(*deletionServiceRow){
		func(row *deletionServiceRow) { row.name = nil },
		func(row *deletionServiceRow) { row.enabled = nil },
		func(row *deletionServiceRow) { row.baseURL = []byte("http://example.com") },
		func(row *deletionServiceRow) { row.tokens = nil },
		func(row *deletionServiceRow) { row.keys = nil },
		func(row *deletionServiceRow) { row.tags = []int64{3} },
	} {
		bad := r
		change(&bad)
		write(bad)
		if _, err := s.lookup(ctx, provider, service); err == nil {
			t.Fatal("corrupt native metadata accepted")
		}
	}
	write(r)
	exec := func(query string, args ...any) {
		t.Helper()
		if err := cassandra.Query(query, args...).WithContext(ctx).Consistency(gocql.LocalQuorum).Exec(); err != nil {
			t.Fatal(err)
		}
	}
	for _, team := range []gocql.UUID{teamA, teamB} {
		for _, sid := range []gocql.UUID{service, other} {
			exec(`INSERT INTO service_whitelist (team,provider,service) VALUES (?,?,?)`, team, provider, sid)
			exec(`INSERT INTO service_whitelist_rev (provider,service,team) VALUES (?,?,?)`, provider, sid, team)
		}
	}
	// Literal native Unicode keys and all seven subsets are independent of the
	// candidate key helper, so a casing/subset mistake leaves observable rows.
	indexName, prefix := "i\u0307σynthetic", "i\u0307"
	for _, sid := range []gocql.UUID{service, other} {
		exec(`INSERT INTO service_prefix (prefix,name,service,provider) VALUES (?,?,?,?)`, prefix, indexName, sid, provider)
		for _, mask := range []int64{1, 2, 3, 4, 5, 6, 7} {
			exec(`INSERT INTO service_tag (bucket,tag,name,service,provider) VALUES (?,?,?,?,?)`, int32(201608), mask, indexName, sid, provider)
		}
	}
	if err := s.remove(ctx, provider, service, *got); err != nil {
		t.Fatal("service metadata cleanup", err)
	}
	if got, err := s.lookup(ctx, provider, service); err != nil || got != nil {
		t.Fatal("service remains", err)
	}
	check := func(want int, query string, args ...any) {
		t.Helper()
		var count int
		if err := cassandra.Query(query, args...).WithContext(ctx).Consistency(gocql.LocalQuorum).Scan(&count); err != nil || count != want {
			t.Fatal("metadata row count", count, want, err)
		}
	}
	for _, sid := range []gocql.UUID{service, other} {
		want := 0
		if sid == other {
			want = 1
		}
		for _, team := range []gocql.UUID{teamA, teamB} {
			check(want, `SELECT count(*) FROM service_whitelist WHERE team=? AND provider=? AND service=?`, team, provider, sid)
		}
		check(2*want, `SELECT count(*) FROM service_whitelist_rev WHERE provider=? AND service=?`, provider, sid)
		check(want, `SELECT count(*) FROM service_prefix WHERE prefix=? AND name=? AND service=?`, prefix, indexName, sid)
		for _, mask := range []int64{1, 2, 3, 4, 5, 6, 7} {
			check(want, `SELECT count(*) FROM service_tag WHERE bucket=? AND tag=? AND name=? AND service=?`, int32(201608), mask, indexName, sid)
		}
	}
	if err := s.remove(ctx, provider, service, *got); err != nil {
		t.Fatal("metadata replay", err)
	}
	// Native corrupt overfull tags fail after whitelist cleanup has committed,
	// while leaving the service row for retry. Do not silently move that phase.
	r.tags = []int64{1, 2, 4, 8}
	write(r)
	exec(`INSERT INTO service_whitelist (team,provider,service) VALUES (?,?,?)`, teamA, provider, service)
	exec(`INSERT INTO service_whitelist_rev (provider,service,team) VALUES (?,?,?)`, provider, service, teamA)
	if err := s.remove(ctx, provider, service, deletionServiceRecord{name: name, tags: r.tags}); err == nil {
		t.Fatal("overfull native tags accepted")
	}
	check(0, `SELECT count(*) FROM service_whitelist WHERE team=? AND provider=? AND service=?`, teamA, provider, service)
	check(0, `SELECT count(*) FROM service_whitelist_rev WHERE provider=? AND service=?`, provider, service)
	check(1, `SELECT count(*) FROM service WHERE provider=? AND id=?`, provider, service)
}
