//go:build brig_storage_qualification

// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

// Called by the existing owned-store runner. Native tools provision the schema;
// this asserts durable native shapes, guarded ownership and real page cleanup.
// It is not a public API or native-Go-native qualification claim.
func qualifyAccountDeletionStores(t *testing.T, ctx context.Context, stores map[string]userStore, record func(gocql.UUID) storedUser) {
	t.Helper()
	newID := func() gocql.UUID {
		id, err := gocql.RandomUUID()
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	cassandra := stores["cassandra"].cassandra
	team := newID()
	for _, mode := range []string{"cassandra", "postgresql", "migration-to-postgresql"} {
		t.Run("deletion-"+mode, func(t *testing.T) {
			s := stores[mode]
			id := newID()
			u := record(id)
			u.Team = &team
			u.Activated = true
			email := id.String() + "@example.com"
			unvalidated := "pending-" + email
			statusText := "Synthetic status"
			u.Email = &email
			u.EmailUnvalidated = &unvalidated
			u.TextStatus = &statusText
			u.Accent = 4
			if err := s.create(ctx, u, nil, nil); err != nil {
				t.Fatal(err)
			}
			if err := s.deleteAccount(ctx, u); err != nil {
				t.Fatal(err)
			}
			got, err := s.get(ctx, id)
			if err != nil || got == nil || got.Status == nil || *got.Status != 2 || got.Name != "default" || got.Accent != 0 || got.Email != nil || got.Handle != nil || len(got.Assets) != 0 || len(got.Picture) != 0 || got.Team == nil || *got.Team != team {
				t.Fatal("native tombstone shape changed", mode, err)
			}
			if mode == "cassandra" && (got.EmailUnvalidated == nil || *got.EmailUnvalidated != unvalidated || got.TextStatus == nil || *got.TextStatus != statusText || !got.Activated) {
				t.Fatal("Cassandra deletion cleared native retained columns")
			}
			if err := s.deleteAccount(ctx, u); err != nil {
				t.Fatal("idempotent deletion", err)
			}
			absent := record(newID())
			if err := s.deleteAccount(ctx, absent); err != nil {
				t.Fatal("missing user deletion", err)
			}
			if mode != "cassandra" {
				var count int
				err := s.postgres.QueryRow(ctx, `SELECT count(*) FROM deleted_user WHERE id=$1::uuid`, absent.ID.String()).Scan(&count)
				if err != nil || count != 0 {
					t.Fatal("deletion invented PG tombstone for missing user", err)
				}
			}
		})
	}
	// Hold a conflicting update until the serializable DELETE is waiting on
	// its row lock. The native transaction retries40001 and creates exactly one
	// deletion record; scrubbing the error inside the transaction loses that
	// retry. This exercises actual PostgreSQL isolation, rather than a mock.
	pg := stores["postgresql"]
	conflict := record(newID())
	if err := pg.create(ctx, conflict, nil, nil); err != nil {
		t.Fatal(err)
	}
	blocker, err := pg.postgres.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(ctx)
	if _, err = blocker.Exec(ctx, `UPDATE wire_user SET name=$2 WHERE id=$1::uuid`, conflict.ID.String(), "Synthetic concurrent update"); err != nil {
		t.Fatal(err)
	}
	completed := make(chan error, 1)
	go func() { completed <- pg.deleteAccount(ctx, conflict) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var waiting bool
		err := pg.postgres.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE 'DELETE FROM wire_user WHERE id=%')`).Scan(&waiting)
		if err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("serializable deletion did not wait for competing update")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err = blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-completed:
		if err != nil {
			t.Fatal("serializable deletion did not retry", err)
		}
	case <-ctx.Done():
		t.Fatal("serializable deletion did not complete")
	}
	var deletedCount int
	if err = pg.postgres.QueryRow(ctx, `SELECT count(*) FROM deleted_user WHERE id=$1::uuid`, conflict.ID.String()).Scan(&deletedCount); err != nil || deletedCount != 1 {
		t.Fatal("serialization retry lost deletion record", err)
	}

	// Migration-mode deletion must also select a Cassandra-resident user,
	// retaining its native row instead of inventing a PostgreSQL tombstone.
	migrate := stores["migration-to-postgresql"]
	legacy := record(newID())
	if err := migrate.createCassandra(ctx, legacy, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := migrate.deleteAccount(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	got, err := stores["cassandra"].get(ctx, legacy.ID)
	if err != nil || got == nil || got.Status == nil || *got.Status != 2 {
		t.Fatal("migration failed Cassandra deletion", err)
	}

	// Preserve reassigned email/handle ownership and other simultaneous claims.
	s := stores["cassandra"]
	owner, other := newID(), newID()
	handle := "synthetic_" + owner.String()[:8]
	u := record(owner)
	u.Handle = &handle
	if err := s.create(ctx, u, nil, nil); err != nil {
		t.Fatal(err)
	}
	query := func(statement string, args ...any) *gocql.Query {
		return cassandra.Query(statement, args...).WithContext(ctx).Consistency(gocql.LocalQuorum)
	}
	if err := query(`INSERT INTO user_handle (handle,user) VALUES (?,?)`, handle, other).Exec(); err != nil {
		t.Fatal(err)
	}
	if err := s.deleteAccount(ctx, u); err != nil {
		t.Fatal(err)
	}
	var remaining gocql.UUID
	if err := query(`SELECT user FROM user_handle WHERE handle=?`, handle).Scan(&remaining); err != nil || remaining != other {
		t.Fatal("reassigned handle deleted", err)
	}
	if err := query(`INSERT INTO user_handle (handle,user) VALUES (?,?)`, handle, owner).Exec(); err != nil {
		t.Fatal(err)
	}
	if err := query(`INSERT INTO unique_claims (value,claims) VALUES (?,?)`, "@"+handle, []gocql.UUID{owner, other}).Exec(); err != nil {
		t.Fatal(err)
	}
	if err := s.deleteAccount(ctx, u); err != nil {
		t.Fatal(err)
	}
	if err := query(`SELECT user FROM user_handle WHERE handle=?`, handle).Scan(&remaining); !errors.Is(err, gocql.ErrNotFound) {
		t.Fatal("owned handle retained", err)
	}
	var claims []gocql.UUID
	if err := query(`SELECT claims FROM unique_claims WHERE value=?`, "@"+handle).Scan(&claims); err != nil || len(claims) != 1 || claims[0] != other {
		t.Fatal("other unique claim deleted", err)
	}
	email := owner.String() + "@example.com"
	key, err := emailUniqueKey(email)
	if err != nil {
		t.Fatal(err)
	}
	if err := query(`INSERT INTO user_keys (key,user) VALUES (?,?)`, key, other).Exec(); err != nil {
		t.Fatal(err)
	}
	if err := s.deleteEmailKey(ctx, email, &owner); err != nil {
		t.Fatal(err)
	}
	if err := query(`SELECT user FROM user_keys WHERE key=?`, key).Scan(&remaining); err != nil || remaining != other {
		t.Fatal("reassigned email deleted", err)
	}

	// More than one native page; remove reverse rows in all statuses and the
	// owner's local/remote partitions while preserving unrelated connections.
	deleting, unrelatedA, unrelatedB := newID(), newID(), newID()
	contacts := make([]gocql.UUID, 101)
	batch := cassandra.NewBatch(gocql.LoggedBatch).WithContext(ctx).Consistency(gocql.LocalQuorum)
	for i := range contacts {
		contacts[i] = newID()
		relation := int32(i % 12)
		batch.Query(`INSERT INTO connection (left,right,status,last_update) VALUES (?,?,?,?)`, deleting, contacts[i], relation, time.Now().UTC())
		batch.Query(`INSERT INTO connection (left,right,status,last_update) VALUES (?,?,?,?)`, contacts[i], deleting, relation, time.Now().UTC())
	}
	batch.Query(`INSERT INTO connection (left,right,status,last_update) VALUES (?,?,?,?)`, unrelatedA, unrelatedB, int32(0), time.Now().UTC())
	batch.Query(`INSERT INTO connection_remote (left,right_domain,right_user,status) VALUES (?,?,?,?)`, deleting, "b.example", newID(), int32(0))
	if err := cassandra.ExecuteBatch(batch); err != nil {
		t.Fatal(err)
	}
	if err := (connectionStore{cassandra: cassandra, domain: "example.com"}).deleteAccount(ctx, deleting); err != nil {
		t.Fatal(err)
	}
	var count int64
	for _, table := range []string{"connection", "connection_remote"} {
		if err := query(`SELECT count(*) FROM `+table+` WHERE left=?`, deleting).Scan(&count); err != nil || count != 0 {
			t.Fatal("own connection partition remains", table, err)
		}
	}
	for _, contact := range contacts {
		if err := query(`SELECT count(*) FROM connection WHERE left=? AND right=?`, contact, deleting).Scan(&count); err != nil || count != 0 {
			t.Fatal("reverse connection remains", err)
		}
	}
	if err := query(`SELECT count(*) FROM connection WHERE left=? AND right=?`, unrelatedA, unrelatedB).Scan(&count); err != nil || count != 1 {
		t.Fatal("unrelated connection deleted", err)
	}
}
