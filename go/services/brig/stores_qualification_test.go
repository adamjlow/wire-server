//go:build brig_storage_qualification

// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

// Built explicitly and executed only inside the owned Docker namespace. The
// regular module suite never silently skips a missing database qualification.
func TestOwnedDurableStores(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	c, err := LoadConfig(os.Getenv("BRIG_STORE_CONFIG"))
	if err != nil {
		t.Fatal(err)
	}
	cassandra, err := openCassandra(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	defer cassandra.Close()
	postgres, err := openPostgres(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	defer postgres.Close()
	if err := runPostgresMigrations(ctx, postgres, time.Duration(c.PostgresPool.Acquisition)); err != nil {
		t.Fatal(err)
	}
	password := "14|8|1|AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=|AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=="
	changedPassword := strings.Replace(password, "14|", "13|", 1)
	typ, status, managed, protocol, size := int32(0), int32(0), int32(0), int32(3), int32(1)
	language, label := "en", "synthetic-device"
	searchable := true
	user := func() gocql.UUID {
		id, err := gocql.RandomUUID()
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	newRecord := func(id gocql.UUID) storedUser {
		return storedUser{ID: id, Type: &typ, Name: "Synthetic user", Status: &status, ManagedBy: &managed, Protocols: &protocol, Language: &language, Searchable: &searchable, Password: &password, Assets: []userAsset{{Key: "3-1-00000000-0000-0000-0000-000000000001", Size: &size}}}
	}
	stores := map[string]userStore{}
	for _, mode := range []string{"cassandra", "postgresql", "migration-to-postgresql"} {
		s := userStore{userAuthStore{cassandra: cassandra, postgres: postgres, mode: mode, acquisition: time.Duration(c.PostgresPool.Acquisition)}}
		stores[mode] = s
		id := user()
		if err := s.create(ctx, newRecord(id), nil, nil); err != nil {
			t.Fatalf("%s create: %v", mode, err)
		}
		r, err := s.get(ctx, id)
		if err != nil || r == nil {
			t.Fatalf("%s get: %v", mode, err)
		}
		if r.Name != "Synthetic user" || r.Type == nil || *r.Type != 0 || r.Status == nil || *r.Status != 0 || len(r.Assets) != 1 || r.Assets[0].Size == nil || *r.Assets[0].Size != 1 || r.Protocols == nil || *r.Protocols != 3 {
			t.Fatalf("%s record marshalling mismatch", mode)
		}
		a, err := s.authentication(ctx, id)
		if err != nil || a == nil || a.Password == nil || *a.Password != password || a.Status != 0 {
			t.Fatalf("%s auth persistence mismatch: %v", mode, err)
		}
		if err := s.setPassword(ctx, id, changedPassword); err != nil {
			t.Fatal(err)
		}
		a, err = s.authentication(ctx, id)
		if err != nil || a == nil || a.Password == nil || *a.Password != changedPassword {
			t.Fatalf("%s password update mismatch: %v", mode, err)
		}
		if mode == "migration-to-postgresql" {
			var exists bool
			if err := postgres.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM wire_user WHERE id=$1::uuid)`, id.String()).Scan(&exists); err != nil || !exists {
				t.Fatal("new migration-mode user not stored in PostgreSQL")
			}
		}
	}
	// Existing Cassandra users stay there until the native migration moves them.
	id := user()
	if err := stores["cassandra"].create(ctx, newRecord(id), nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := stores["migration-to-postgresql"].setPassword(ctx, id, password); err != nil {
		t.Fatal(err)
	}
	if err := stores["migration-to-postgresql"].create(ctx, newRecord(id), nil, nil); err != nil {
		t.Fatal(err)
	}
	var inPostgres bool
	if err := postgres.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM wire_user WHERE id=$1::uuid)`, id.String()).Scan(&inPostgres); err != nil || inPostgres {
		t.Fatal("existing Cassandra user unexpectedly copied to PostgreSQL")
	}
	// Native tombstones must take precedence over a stale live PG record.
	if err := stores["postgresql"].create(ctx, newRecord(id), nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := postgres.Exec(ctx, `INSERT INTO deleted_user(id,created_at) VALUES($1::uuid,now())`, id.String()); err != nil {
		t.Fatal(err)
	}
	r, err := stores["migration-to-postgresql"].get(ctx, id)
	if err != nil || r == nil || r.Status == nil || *r.Status != 2 || r.Email != nil || r.Name != "default" {
		t.Fatal("deleted-user precedence mismatch")
	}
	// A real held exclusive advisory lock prevents concurrent migration reads.
	lease, err := postgres.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	key := userMigrationLockKey(id)
	if _, err := lease.Exec(ctx, `SELECT pg_advisory_lock($1::bigint)`, key); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	_, err = stores["migration-to-postgresql"].authentication(ctx, id)
	if err == nil || time.Since(started) > 2*time.Second {
		t.Fatal("migration lock acquisition did not enforce native deadline")
	}
	if _, err := lease.Exec(ctx, `SELECT pg_advisory_unlock($1::bigint)`, key); err != nil {
		t.Fatal(err)
	}
	// Cookie ID signedness/null successor/label, millisecond time, TTL override
	// and exact-key revocation exercise the actual native Cassandra table.
	sessions := sessionStore{cassandra}
	now := time.Now().UTC().Truncate(time.Millisecond)
	cookie := sessionCookie{ID: 0xffffffff, Type: 1, Created: now, Expires: now.Add(time.Hour), Label: &label}
	ttl := int32(30)
	if err := sessions.insert(ctx, id, cookie, &ttl); err != nil {
		t.Fatal(err)
	}
	got, err := sessions.lookup(ctx, id, cookie.Expires, cookie.ID)
	if err != nil || got == nil || got.ID != cookie.ID || got.Label == nil || *got.Label != label || got.Successor != nil || !got.Created.Equal(now) {
		t.Fatal("cookie persistence mismatch")
	}
	var actualTTL int32
	if err := cassandra.Query(`SELECT TTL(type) FROM user_cookies WHERE user=? AND expires=? AND id=?`, id, cookie.Expires, int64(cookie.ID)).WithContext(ctx).Scan(&actualTTL); err != nil || actualTTL <= 0 || actualTTL > ttl {
		t.Fatal("cookie TTL override mismatch")
	}
	next := uint32(0)
	cookie.Successor = &next
	if err := sessions.insert(ctx, id, cookie, nil); err != nil {
		t.Fatal(err)
	}
	cc, err := sessions.list(ctx, id)
	if err != nil || len(cc) != 1 || cc[0].Successor == nil || *cc[0].Successor != 0 {
		t.Fatal("cookie successor mismatch")
	}
	if err := sessions.delete(ctx, id, cc); err != nil {
		t.Fatal(err)
	}
	if got, err := sessions.lookup(ctx, id, cookie.Expires, cookie.ID); err != nil || got != nil {
		t.Fatal("revoked cookie still exists")
	}
	if err := sessions.insert(ctx, id, cookie, nil); err != nil {
		t.Fatal(err)
	}
	if err := sessions.deleteAll(ctx, id); err != nil {
		t.Fatal(err)
	}
	if cc, err := sessions.list(ctx, id); err != nil || len(cc) != 0 {
		t.Fatal("all-cookie revocation failed")
	}
}
