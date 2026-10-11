//go:build brig_storage_qualification

// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"testing"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func qualifyDeletionCodeStores(t *testing.T, ctx context.Context, cassandra *gocql.Session) {
	t.Helper()
	s := verificationCodeStore{cassandra}
	email := "deletion-codes@example.com"
	key, err := emailVerificationKey(email)
	if err != nil {
		t.Fatal(err)
	}
	account := gocql.TimeUUID()
	row := deletionVerificationCode{key: key, value: "01234567890123456789", email: email, ttl: 120, retries: 3, account: &account}
	if err := s.deletionInsert(ctx, row); err != nil {
		t.Fatal(err)
	}
	got, err := s.deletionLookup(ctx, key)
	if err != nil || got == nil || got.account == nil || *got.account != account || got.value != row.value || got.email != email || got.retries != 3 || got.ttl < 110 || got.ttl > 120 {
		t.Fatal("native code fields/TTL", got, err)
	}
	service := accountDeletionCodes{lookup: s.deletionLookup, insert: s.deletionInsert}
	if code, err := service.verify(ctx, key, "incorrect"); err != nil || code != nil {
		t.Fatal("wrong verification", err)
	}
	got, err = s.deletionLookup(ctx, key)
	if err != nil || got == nil || got.retries != 2 || got.ttl < 100 || got.ttl > 120 || got.account == nil || *got.account != account {
		t.Fatal("remaining TTL/retry/account", got, err)
	}
	if code, err := service.verify(ctx, key, row.value); err != nil || code == nil {
		t.Fatal("correct verification", err)
	}
	got, err = s.deletionLookup(ctx, key)
	if err != nil || got == nil || got.retries != 2 {
		t.Fatal("success consumed code", got, err)
	}
	// Another scope sharing the deterministic key survives scope1 removal.
	if err := cassandra.Query(`INSERT INTO vcodes (key,scope,value,retries,email,account) VALUES (?,?,?,?,?,?) USING TTL ?`, key, int32(6), row.value, int32(3), email, account, int32(120)).WithContext(ctx).Consistency(gocql.LocalQuorum).Exec(); err != nil {
		t.Fatal(err)
	}
	if err := s.deletionInsertThrottle(ctx, key, 60); err != nil {
		t.Fatal(err)
	}
	if ttl, err := s.deletionThrottle(ctx, key); err != nil || ttl == nil || *ttl < 50 || *ttl > 60 {
		t.Fatal("native throttle TTL", ttl, err)
	}
	if err := s.deletionRemove(ctx, key); err != nil {
		t.Fatal(err)
	}
	if code, err := s.deletionLookup(ctx, key); err != nil || code != nil {
		t.Fatal("code retained after removal", err)
	}
	if ttl, err := s.deletionThrottle(ctx, key); err != nil || ttl == nil {
		t.Fatal("code removal cleared throttle", err)
	}
	var value string
	if err := cassandra.Query(`SELECT value FROM vcodes WHERE key=? AND scope=?`, key, int32(6)).WithContext(ctx).Consistency(gocql.LocalQuorum).Scan(&value); err != nil || value != row.value {
		t.Fatal("other scope cleared", err)
	}
	// Exhausted retries and native Int32→Word8 wrapping are durable, not mocks.
	if err := cassandra.Query(`INSERT INTO vcodes (key,scope,value,retries,email,account) VALUES (?,?,?,?,?,?) USING TTL ?`, key, int32(1), row.value, int32(256), email, account, int32(120)).WithContext(ctx).Consistency(gocql.LocalQuorum).Exec(); err != nil {
		t.Fatal(err)
	}
	if code, err := service.verify(ctx, key, row.value); err != nil || code != nil {
		t.Fatal("wrapped exhausted code accepted", err)
	}
	// A nonnull initial_delay with no TTL exercises native required TTL decoding.
	if err := cassandra.Query(`INSERT INTO vcodes_throttle (key,scope,initial_delay) VALUES (?,?,?)`, "null-throttle", int32(1), int32(60)).WithContext(ctx).Consistency(gocql.LocalQuorum).Exec(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.deletionThrottle(ctx, "null-throttle"); err == nil {
		t.Fatal("null stored TTL accepted")
	}
}
