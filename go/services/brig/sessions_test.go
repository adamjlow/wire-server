// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func TestCookieEvictionPreservesNativePriorities(t *testing.T) {
	now := time.Unix(100, 0)
	successor := uint32(42)
	// Actual storage order: expiration ascending, then clustering cookie ID.
	cookies := []sessionCookie{
		{ID: 1, Expires: now.Add(-2 * time.Second), Created: now.Add(-4 * time.Second)},
		{ID: 2, Expires: now.Add(-time.Second), Created: now.Add(-3 * time.Second)},
		{ID: 3, Expires: now, Created: now.Add(-2 * time.Second)},
		{ID: 4, Expires: now.Add(time.Second), Created: now.Add(-time.Second), Successor: &successor},
	}
	if got := cookiesToEvict(5, now, cookies); len(got) != 0 {
		t.Fatal("cookies revoked below configured limit")
	}
	got := cookiesToEvict(2, now, cookies)
	if len(got) != 2 || got[0].ID != 1 || got[1].ID != 2 {
		t.Fatal("expired prefix order or native excess count changed")
	}
	got = cookiesToEvict(0, now, cookies[2:])
	if len(got) != 2 || got[0].ID != 4 || got[1].ID != 3 {
		t.Fatal("superseded cookies must precede older live cookies")
	}
	// Expiry equality is not 'expired', consistent with token verification.
	got = cookiesToEvict(2, now, cookies[2:])
	if len(got) != 1 || got[0].ID != 4 {
		t.Fatal("expiry equality changed native eviction semantics")
	}
	if cookies[0].ID != 1 || cookies[2].ID != 3 {
		t.Fatal("eviction selection changed original storage order")
	}
}

func TestUserMigrationLockUUIDFolding(t *testing.T) {
	for _, test := range []struct {
		UUID string
		Key  int64
	}{
		{"00000000-0000-0000-0000-000000000000", 0},
		{"ffffffff-ffff-ffff-ffff-ffffffffffff", 0},
		{"ffffffff-ffff-ffff-0000-000000000000", -1},
		{"00000000-0000-0000-0000-000000000001", 1 << 32},
		{"00000000-0000-0001-0000-000100000000", 0},
	} {
		user, err := gocql.ParseUUID(test.UUID)
		if err != nil {
			t.Fatal(err)
		}
		if got := userMigrationLockKey(user); got != test.Key {
			t.Fatalf("migration UUID lock mismatch: %d != %d", got, test.Key)
		}
	}
}
