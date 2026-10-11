//go:build brig_storage_qualification

// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func qualifyRemoteDeletionStores(t *testing.T, ctx context.Context, s connectionStore) {
	t.Helper()
	deleted, other := gocql.TimeUUID(), gocql.TimeUUID()
	var locals []gocql.UUID
	for i := 0; i < 17; i++ {
		user := gocql.TimeUUID()
		locals = append(locals, user)
		for _, pair := range []struct {
			target gocql.UUID
			domain string
		}{{deleted, "remote.example.com"}, {other, "remote.example.com"}, {deleted, "other.example.com"}} {
			if err := s.cassandra.Query(`INSERT INTO connection_remote(left,right_domain,right_user,status,last_update,conv_domain,conv_id) VALUES(?,?,?,?,?,?,?)`, user, pair.domain, pair.target, int32(i%12), time.Now().UTC(), s.domain, gocql.TimeUUID()).WithContext(ctx).Consistency(gocql.LocalQuorum).Exec(); err != nil {
				t.Fatal(err)
			}
		}
	}
	locals = append(locals, locals[0])
	accepted, err := s.remoteDeletionAccepted(ctx, "remote.example.com", deleted, locals)
	if err != nil || !reflect.DeepEqual(accepted, []gocql.UUID{locals[0], locals[12], locals[0]}) {
		t.Fatal("raw accepted projection/input order changed", accepted, err)
	}
	if err := s.removeRemoteDeletedUser(ctx, "remote.example.com", deleted, locals); err != nil {
		t.Fatal(err)
	}
	for _, user := range locals[:17] {
		var gone, otherUser, otherDomain int
		if err := s.cassandra.Query(`SELECT count(*) FROM connection_remote WHERE left=? AND right_domain=? AND right_user=?`, user, "remote.example.com", deleted).WithContext(ctx).Consistency(gocql.LocalQuorum).Scan(&gone); err != nil {
			t.Fatal(err)
		}
		if err := s.cassandra.Query(`SELECT count(*) FROM connection_remote WHERE left=? AND right_domain=? AND right_user=?`, user, "remote.example.com", other).WithContext(ctx).Consistency(gocql.LocalQuorum).Scan(&otherUser); err != nil {
			t.Fatal(err)
		}
		if err := s.cassandra.Query(`SELECT count(*) FROM connection_remote WHERE left=? AND right_domain=? AND right_user=?`, user, "other.example.com", deleted).WithContext(ctx).Consistency(gocql.LocalQuorum).Scan(&otherDomain); err != nil {
			t.Fatal(err)
		}
		if gone != 0 || otherUser != 1 || otherDomain != 1 {
			t.Fatal("remote deletion scope changed", gone, otherUser, otherDomain)
		}
	}
	if err := s.cassandra.Query(`INSERT INTO connection_remote(left,right_domain,right_user,status) VALUES(?,?,?,?)`, locals[0], "remote.example.com", deleted, int32(12)).WithContext(ctx).Consistency(gocql.LocalQuorum).Exec(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.remoteDeletionAccepted(ctx, "remote.example.com", deleted, locals[:1]); err == nil {
		t.Fatal("invalid stored relation accepted")
	}
}
