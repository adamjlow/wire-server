//go:build brig_storage_qualification

// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
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
		if exists, err := s.exists(ctx, id); err != nil || !exists {
			t.Fatalf("%s existing user lookup: %v", mode, err)
		}
		if exists, err := s.exists(ctx, user()); err != nil || exists {
			t.Fatalf("%s missing user lookup: %v", mode, err)
		}
		if err := s.updateAccountStatus(ctx, id, 1); err != nil {
			t.Fatalf("%s suspend status write: %v", mode, err)
		}
		indexed, err := s.indexUser(ctx, id)
		if err != nil || indexed == nil || indexed.user.Name != "Synthetic user" || indexed.user.Status == nil || *indexed.user.Status != 1 || indexed.user.Type == nil || *indexed.user.Type != 0 || indexed.updated.Before(indexed.created) {
			t.Fatalf("%s native index storage mismatch: %v", mode, err)
		}
		if missing, err := s.indexUser(ctx, user()); err != nil || missing != nil {
			t.Fatalf("%s missing index user mismatch: %v", mode, err)
		}
		a, err = s.authentication(ctx, id)
		if err != nil || a == nil || a.Status != 1 || a.Password == nil || *a.Password != changedPassword {
			t.Fatalf("%s suspend changed unrelated auth state: %v", mode, err)
		}
		if err := s.updateAccountStatus(ctx, id, 0); err != nil {
			t.Fatalf("%s resume status write: %v", mode, err)
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
	if exists, err := stores["migration-to-postgresql"].exists(ctx, id); err != nil || !exists {
		t.Fatal("migration-mode existence did not find legacy Cassandra user", err)
	}
	if err := stores["migration-to-postgresql"].updateAccountStatus(ctx, id, 1); err != nil {
		t.Fatal(err)
	}
	legacyAuth, err := stores["cassandra"].authentication(ctx, id)
	if err != nil || legacyAuth == nil || legacyAuth.Status != 1 {
		t.Fatal("migration-mode status write did not stay in Cassandra", err)
	}
	legacyIndex, err := stores["migration-to-postgresql"].indexUser(ctx, id)
	if err != nil || legacyIndex == nil || legacyIndex.user.Status == nil || *legacyIndex.user.Status != 1 {
		t.Fatal("migration-mode index lookup did not use legacy Cassandra", err)
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
	for _, mode := range []string{"postgresql", "migration-to-postgresql"} {
		if exists, err := stores[mode].exists(ctx, id); err != nil || !exists {
			t.Fatal("native existence excludes deletion tombstone", mode, err)
		}
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
	// Explicit native CQL timestamps demonstrate activated creation time and
	// write_time_bumper's contribution to the external search document version.
	indexID := user()
	createdMicros, updatedMicros := int64(1700000000123456), int64(1700000001123456)
	if err := cassandra.Query(`INSERT INTO user (id,name,accent_id,activated,status) VALUES (?,?,?,?,?) USING TIMESTAMP ?`, indexID, "Synthetic index", int32(2), true, int32(0), createdMicros).WithContext(ctx).Exec(); err != nil {
		t.Fatal(err)
	}
	if err := cassandra.Query(`UPDATE user USING TIMESTAMP ? SET write_time_bumper=? WHERE id=?`, updatedMicros, int32(0), indexID).WithContext(ctx).Exec(); err != nil {
		t.Fatal(err)
	}
	indexed, err := stores["cassandra"].indexUser(ctx, indexID)
	if err != nil || indexed == nil || indexed.created.UnixMicro() != createdMicros || indexed.updated.UnixMicro() != updatedMicros || indexed.user.Type == nil || *indexed.user.Type != 0 {
		t.Fatal("native index timestamps/default type", err)
	}
	// Malformed legacy rows fail before an incomplete search document is used.
	partialID := user()
	if err := cassandra.Query(`INSERT INTO user (id,name) VALUES (?,?)`, partialID, "Synthetic partial").WithContext(ctx).Exec(); err != nil {
		t.Fatal(err)
	}
	if _, err := stores["cassandra"].indexUser(ctx, partialID); err == nil {
		t.Fatal("incomplete native index row accepted")
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
	// Client/prekey/MLS reads use Cassandra in every configured user mode.
	clientOwner := user()
	created := time.Unix(1700000000, 123000000).UTC()
	class := int32(2)
	for _, row := range []struct {
		id    string
		typ   int32
		class *int32
		caps  []int32
	}{{"1", 1, &class, []int32{1, 2}}, {"2", 0, nil, nil}} {
		if err := cassandra.Query(`INSERT INTO clients (user,client,type,tstamp,class,capabilities) VALUES (?,?,?,?,?,?)`, clientOwner, row.id, row.typ, created, row.class, row.caps).WithContext(ctx).Consistency(gocql.LocalQuorum).Exec(); err != nil {
			t.Fatal(err)
		}
	}
	for _, scheme := range []string{"ed25519", "ecdsa_secp256r1_sha256", "ecdsa_secp384r1_sha384", "ecdsa_secp521r1_sha512"} {
		if err := cassandra.Query(`INSERT INTO mls_public_keys (user,client,sig_scheme,key) VALUES (?,?,?,?)`, clientOwner, "1", scheme, []byte{0, 255}).WithContext(ctx).Consistency(gocql.LocalQuorum).Exec(); err != nil {
			t.Fatal(err)
		}
	}
	clients := clientStore{cassandra}
	gotClient, err := clients.get(ctx, clientOwner, "00001")
	if err != nil || gotClient == nil || gotClient.ID != "1" || gotClient.Class == nil || *gotClient.Class != 2 || len(gotClient.Capabilities) != 2 || len(gotClient.PublicKeys) != 4 || !gotClient.Created.Equal(created) {
		t.Fatal("actual native client/MLS fields not preserved", err)
	}
	listed, err := clients.list(ctx, clientOwner)
	if err != nil || len(listed) != 2 || listed[0].ID != "1" || listed[1].ID != "2" || listed[1].Class != nil || len(listed[1].Capabilities) != 0 || len(listed[1].PublicKeys) != 0 {
		t.Fatal("actual client order/null defaults changed", err)
	}
	if absent, err := clients.get(ctx, clientOwner, "ff"); err != nil || absent != nil {
		t.Fatal("missing client lookup changed", err)
	}
	for _, key := range []int32{1, 65535} {
		if err := cassandra.Query(`INSERT INTO prekeys (user,client,key,data) VALUES (?,?,?,?)`, clientOwner, "1", key, "synthetic-prekey-text").WithContext(ctx).Consistency(gocql.LocalQuorum).Exec(); err != nil {
			t.Fatal(err)
		}
	}
	ids, err := clients.prekeyIDs(ctx, clientOwner, "0001")
	if err != nil || len(ids) != 2 || ids[0] != 1 || ids[1] != 65535 {
		t.Fatal("actual prekey ID order/width changed", err)
	}
	if err := cassandra.Query(`INSERT INTO mls_public_keys (user,client,sig_scheme,key) VALUES (?,?,?,?)`, clientOwner, "3", "invalid-synthetic-scheme", []byte{0}).WithContext(ctx).Consistency(gocql.LocalQuorum).Exec(); err != nil {
		t.Fatal(err)
	}
	if _, err := clients.get(ctx, clientOwner, "3"); err == nil {
		t.Fatal("malformed orphan MLS key hidden by missing client")
	}
	// Email identity and activation/budgets remain Cassandra contracts even
	// when PostgreSQL owns the user. Handle lookup has native PG precedence.
	handle := "synthetic-login-handle"
	cassOwner, pgOwner := user(), user()
	if err := stores["postgresql"].create(ctx, newRecord(pgOwner), nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := postgres.Exec(ctx, `UPDATE wire_user SET handle=$1 WHERE id=$2::uuid`, handle, pgOwner.String()); err != nil {
		t.Fatal(err)
	}
	if err := cassandra.Query(`INSERT INTO user_handle (handle,user) VALUES (?,?)`, handle, cassOwner).WithContext(ctx).Consistency(gocql.LocalQuorum).Exec(); err != nil {
		t.Fatal(err)
	}
	email := "Synthetic+ignored@Example.com"
	if err := cassandra.Query(`INSERT INTO user_keys (key,user) VALUES (?,?)`, "synthetic@example.com", cassOwner).WithContext(ctx).Consistency(gocql.LocalQuorum).Exec(); err != nil {
		t.Fatal(err)
	}
	for mode, store := range stores {
		got, err := store.resolveLogin(ctx, loginIdentifier{email: &email})
		if err != nil || got == nil || *got != cassOwner {
			t.Fatal(mode, "email key did not remain Cassandra", err)
		}
		got, err = store.resolveLogin(ctx, loginIdentifier{handle: &handle})
		want := pgOwner
		if mode == "cassandra" {
			want = cassOwner
		}
		if err != nil || got == nil || *got != want {
			t.Fatal(mode, "handle backend precedence changed", err)
		}
		missing := "synthetic-missing-handle"
		if got, err := store.resolveLogin(ctx, loginIdentifier{handle: &missing}); err != nil || got != nil {
			t.Fatal(mode, "missing handle lookup", err)
		}
		pendingEmail := "synthetic-pending-" + mode + "@example.com"
		digest := sha256.Sum256([]byte(pendingEmail))
		key := base64.URLEncoding.EncodeToString(digest[:])
		pendingUser := user()
		if err := store.create(ctx, newRecord(pendingUser), nil, nil); err != nil {
			t.Fatal(err)
		}
		if err := cassandra.Query(`INSERT INTO activation_keys (key,key_type,key_text,code,user,retries) VALUES (?,?,?,?,?,?)`, key, "email", pendingEmail, "123456", pendingUser, int32(3)).WithContext(ctx).Consistency(gocql.LocalQuorum).Exec(); err != nil {
			t.Fatal(err)
		}
		if pending, err := store.pendingLoginActivation(ctx, pendingEmail); err != nil || !pending {
			t.Fatal(mode, "unactivated user not pending", err)
		}
		if err := store.updateAccountStatus(ctx, pendingUser, 1); err != nil {
			t.Fatal(err)
		}
		if pending, err := store.pendingLoginActivation(ctx, pendingEmail); err != nil || pending {
			t.Fatal(mode, "suspended user returned pending", err)
		}
	}
	fallback := "synthetic-cassandra-only-handle"
	if err := cassandra.Query(`INSERT INTO user_handle (handle,user) VALUES (?,?)`, fallback, cassOwner).WithContext(ctx).Consistency(gocql.LocalQuorum).Exec(); err != nil {
		t.Fatal(err)
	}
	if got, err := stores["migration-to-postgresql"].resolveLogin(ctx, loginIdentifier{handle: &fallback}); err != nil || got == nil || *got != cassOwner {
		t.Fatal("migrating handle did not fall back", err)
	}
	budgetStore := loginBudgetStore{cassandra}
	budgetKey := "login#" + user().String()
	if got, err := budgetStore.lookup(ctx, budgetKey); err != nil || got != nil {
		t.Fatal("missing budget", err)
	}
	if err := budgetStore.insert(ctx, budgetKey, loginBudget{seconds: 30.5, remaining: 2}); err != nil {
		t.Fatal(err)
	}
	budget, err := budgetStore.lookup(ctx, budgetKey)
	if err != nil || budget == nil || budget.remaining != 2 || budget.seconds <= 0 || budget.seconds > 30 {
		t.Fatal("budget value/TTL native rounding", err)
	}
	budget.remaining--
	if err := budgetStore.insert(ctx, budgetKey, *budget); err != nil {
		t.Fatal(err)
	}
	nextBudget, err := budgetStore.lookup(ctx, budgetKey)
	if err != nil || nextBudget == nil || nextBudget.remaining != 1 || nextBudget.seconds > budget.seconds {
		t.Fatal("budget reset timeout or lost retry", err)
	}
	// Activation/email ownership share the unchanged native tables across all
	// user modes. Reject stealing an active identity and wrong-owner deletion.
	for mode, store := range stores {
		owner := user()
		if err := store.create(ctx, newRecord(owner), nil, nil); err != nil {
			t.Fatal(err)
		}
		if active, err := store.activated(ctx, owner); err != nil || active {
			t.Fatal(mode, "new user unexpectedly activated", err)
		}
		email := "Synthetic+alias-" + mode + "@example.com"
		if claimed, err := store.claimEmailKey(ctx, email, owner); err != nil || !claimed {
			t.Fatal(mode, "claim unactivated identity", err)
		}
		if err := store.setEmailIdentity(ctx, owner, &email, true); err != nil {
			t.Fatal(mode, err)
		}
		if active, err := store.activated(ctx, owner); err != nil || !active {
			t.Fatal(mode, "activation not persisted", err)
		}
		other := user()
		if available, err := store.emailKeyAvailable(ctx, email, &other); err != nil || available {
			t.Fatal(mode, "active email available to other owner", err)
		}
		if claimed, err := store.claimEmailKey(ctx, email, other); err != nil || claimed {
			t.Fatal(mode, "active identity stolen", err)
		}
		if err := store.deleteEmailKey(ctx, email, &other); err != nil {
			t.Fatal(err)
		}
		if available, err := store.emailKeyAvailable(ctx, email, &other); err != nil || available {
			t.Fatal("wrong-owner deletion removed identity", err)
		}
		if claimed, err := store.claimEmailKey(ctx, email, owner); err != nil || !claimed {
			t.Fatal("same-owner claim failed", err)
		}
		if err := store.clearUnvalidatedEmail(ctx, owner); err != nil {
			t.Fatal(err)
		}
		account, err := store.get(ctx, owner)
		if err != nil || account == nil || !account.Activated || account.Email == nil || *account.Email != email || account.EmailUnvalidated != nil {
			t.Fatal(mode, "activated identity projection changed", err)
		}
		if err := store.deleteEmailKey(ctx, email, &owner); err != nil {
			t.Fatal(err)
		}
		if available, err := store.emailKeyAvailable(ctx, email, nil); err != nil || !available {
			t.Fatal("released email unavailable", err)
		}
	}
	legacyOwner := user()
	if err := stores["cassandra"].create(ctx, newRecord(legacyOwner), nil, nil); err != nil {
		t.Fatal(err)
	}
	legacyEmail := "synthetic-legacy-activation@example.com"
	if err := stores["migration-to-postgresql"].setEmailIdentity(ctx, legacyOwner, &legacyEmail, true); err != nil {
		t.Fatal(err)
	}
	if active, err := stores["cassandra"].activated(ctx, legacyOwner); err != nil || !active {
		t.Fatal("legacy activation migrated user implicitly", err)
	}
	activation := activationStore{cassandra}
	record, err := activation.newCode(ctx, "synthetic-activation@example.com", 60.5, &legacyOwner)
	if err != nil || len(record.code) != 6 || record.ttl != 60 || record.retries != 3 {
		t.Fatal("native activation generation shape", err)
	}
	gotActivation, err := activation.verify(ctx, record.key, record.code)
	if err != nil || gotActivation == nil || gotActivation.user == nil || *gotActivation.user != legacyOwner {
		t.Fatal("generated activation could not be verified", err)
	}
	for expected := int32(2); expected >= 0; expected-- {
		if _, err := activation.verify(ctx, record.key, "wrong-synthetic-code"); !errors.Is(err, errInvalidActivationCode) {
			t.Fatal("wrong activation accepted", err)
		}
		stored, err := activation.lookup(ctx, record.key)
		if err != nil || stored == nil || stored.retries != expected || stored.ttl > record.ttl || stored.ttl <= 0 {
			t.Fatal("activation retry reset TTL or deleted too early", err)
		}
	}
	if _, err := activation.verify(ctx, record.key, record.code); err != nil {
		t.Fatal("native zero-retry correct code rejected", err)
	}
	if _, err := activation.verify(ctx, record.key, "wrong-synthetic-code"); !errors.Is(err, errInvalidActivationCode) {
		t.Fatal(err)
	}
	if stored, err := activation.lookup(ctx, record.key); err != nil || stored != nil {
		t.Fatal("exhausted activation record not revoked", err)
	}
	for _, mode := range []string{"cassandra", "postgresql", "migration-to-postgresql"} {
		blocked := blocklistStore{databases: stores["postgresql"].userAuthStore, mode: mode}
		email := "synthetic-blocked-" + mode + "@example.com"
		if exists, err := blocked.exists(ctx, email); err != nil || exists {
			t.Fatal("new blocklist row exists", err)
		}
		if err := blocked.write(ctx, email, false); err != nil {
			t.Fatal(err)
		}
		if exists, err := blocked.exists(ctx, email); err != nil || !exists {
			t.Fatal("blocklist write missing", err)
		}
		if mode == "migration-to-postgresql" {
			if exists, err := (blocklistStore{databases: blocked.databases, mode: "postgresql"}).exists(ctx, email); err != nil || !exists {
				t.Fatal("dual-write missed PostgreSQL", err)
			}
		}
		if err := blocked.write(ctx, email, true); err != nil {
			t.Fatal(err)
		}
		if exists, err := blocked.exists(ctx, email); err != nil || exists {
			t.Fatal("blocklist deletion missing", err)
		}
	}
	pgOnlyEmail := "synthetic-pg-only-blocklist@example.com"
	pgOnly := blocklistStore{databases: stores["postgresql"].userAuthStore, mode: "postgresql"}
	if err := pgOnly.write(ctx, pgOnlyEmail, false); err != nil {
		t.Fatal(err)
	}
	if exists, err := (blocklistStore{databases: pgOnly.databases, mode: "migration-to-postgresql"}).exists(ctx, pgOnlyEmail); err != nil || exists {
		t.Fatal("migration blocklist incorrectly read PG", err)
	}
	for mode, store := range stores {
		owner := user()
		if err := store.create(ctx, newRecord(owner), nil, nil); err != nil {
			t.Fatal(err)
		}
		reset := passwordResetStore{databases: store.userAuthStore, mode: mode}
		key := passwordResetKey(owner)
		if got, err := reset.lookup(ctx, key); err != nil || got != nil {
			t.Fatal(mode, "missing password reset", err)
		}
		code, err := newPasswordResetCode()
		if err != nil {
			t.Fatal(err)
		}
		retries := int32(3)
		timeout := time.Now().UTC().Truncate(time.Millisecond).Add(time.Minute)
		record := passwordResetRecord{code: code, user: owner, retries: &retries, timeout: &timeout}
		if err := reset.insert(ctx, key, record, 30); err != nil {
			t.Fatal(mode, "reset insertion", err)
		}
		got, err := reset.lookup(ctx, key)
		if err != nil || got == nil || got.code != code || got.user != owner || got.retries == nil || *got.retries != 3 || got.timeout == nil || !got.timeout.Equal(timeout) {
			t.Fatal(mode, "reset record changed", err)
		}
		if mode == "migration-to-postgresql" {
			if got, err := (passwordResetStore{databases: reset.databases, mode: "postgresql"}).lookup(ctx, key); err != nil || got == nil || got.code != code {
				t.Fatal("reset dual-write missed PG", err)
			}
		}
		service := activationService{codes: activation, account: store.get, identities: store.userAuthStore, deletePasswordReset: reset.deleteForUser}
		email := "synthetic-flow-" + mode + "@example.com"
		activationRecord, err := activation.newCode(ctx, email, 60, &owner)
		if err != nil {
			t.Fatal(err)
		}
		first, err := service.activateKey(ctx, activationRecord.key, activationRecord.code, nil)
		if err != nil || first == nil || !first.first {
			t.Fatal(mode, "real first activation failed", err)
		}
		if again, err := service.activateKey(ctx, activationRecord.key, activationRecord.code, nil); err != nil || again != nil {
			t.Fatal(mode, "repeat activation mutated account", err)
		}
		if got, err := reset.lookup(ctx, key); err != nil || got == nil {
			t.Fatal("first/repeat activation invalidated reset", err)
		}
		newEmail := "synthetic-replaced-" + mode + "@example.com"
		activationRecord, err = activation.newCode(ctx, newEmail, 60, &owner)
		if err != nil {
			t.Fatal(err)
		}
		changed, err := service.activateKey(ctx, activationRecord.key, activationRecord.code, nil)
		if err != nil || changed == nil || changed.first || changed.email != newEmail {
			t.Fatal(mode, "real email replacement failed", err)
		}
		if got, err := reset.lookup(ctx, key); err != nil || got != nil {
			t.Fatal(mode, "email replacement retained reset", err)
		}
		if got, err := store.resolveLogin(ctx, loginIdentifier{email: &email}); err != nil || got != nil {
			t.Fatal(mode, "old email key remained claimed", err)
		}
		if got, err := store.resolveLogin(ctx, loginIdentifier{email: &newEmail}); err != nil || got == nil || *got != owner {
			t.Fatal(mode, "replacement identity not claimed", err)
		}
		if mode == "migration-to-postgresql" {
			if got, err := (passwordResetStore{databases: reset.databases, mode: "postgresql"}).lookup(ctx, key); err != nil || got != nil {
				t.Fatal("reset dual-delete missed PG", err)
			}
		}
	}
	// PG expiration is a query filter, while migrating reads remain Cassandra.
	resetDB := stores["postgresql"].userAuthStore
	pgReset := passwordResetStore{databases: resetDB, mode: "postgresql"}
	pgResetUser := user()
	pgResetKey := passwordResetKey(pgResetUser)
	retries := int32(3)
	timeout := time.Now().UTC().Add(time.Minute)
	if err := pgReset.insert(ctx, pgResetKey, passwordResetRecord{code: "synthetic-code", user: pgResetUser, retries: &retries, timeout: &timeout}, 30); err != nil {
		t.Fatal(err)
	}
	if got, err := (passwordResetStore{databases: resetDB, mode: "migration-to-postgresql"}).lookup(ctx, pgResetKey); err != nil || got != nil {
		t.Fatal("migration reset read incorrectly preferred PG", err)
	}
	if _, err := postgres.Exec(ctx, `UPDATE password_reset SET expires_at=now() WHERE key=$1`, pgResetKey); err != nil {
		t.Fatal(err)
	}
	if got, err := pgReset.lookup(ctx, pgResetKey); err != nil || got != nil {
		t.Fatal("expired PG reset accepted", err)
	}
	// Only retries/timeout are nullable in native PRQueryData Maybe. Never
	// manufacture an empty code or zero UUID from missing required columns.
	legacyResetKey := "synthetic-legacy-nullable-reset"
	if err := cassandra.Query(`INSERT INTO password_reset (key,code,user) VALUES (?,?,?)`, legacyResetKey, "synthetic-code", pgResetUser).WithContext(ctx).Consistency(gocql.LocalQuorum).Exec(); err != nil {
		t.Fatal(err)
	}
	cassReset := passwordResetStore{databases: resetDB, mode: "cassandra"}
	if got, err := cassReset.lookup(ctx, legacyResetKey); err != nil || got == nil || got.retries != nil || got.timeout != nil {
		t.Fatal("legacy nullable reset fields rejected", err)
	}
	for _, column := range []string{"code", "user"} {
		partialKey := "synthetic-missing-" + column + "-reset"
		query := `INSERT INTO password_reset (key,code) VALUES (?,?)`
		value := any("synthetic-code")
		if column == "code" {
			query = `INSERT INTO password_reset (key,user) VALUES (?,?)`
			value = pgResetUser
		}
		if err := cassandra.Query(query, partialKey, value).WithContext(ctx).Consistency(gocql.LocalQuorum).Exec(); err != nil {
			t.Fatal(err)
		}
		if _, err := cassReset.lookup(ctx, partialKey); err == nil {
			t.Fatal("missing required reset column accepted", column)
		}
	}

	// Domain registration migration reads Cassandra regardless of PG presence.
	domainDB := stores["cassandra"].userAuthStore
	for _, mode := range []string{"cassandra", "postgresql", "migration-to-postgresql"} {
		domain := strings.ReplaceAll(mode, "-", ".") + ".synthetic.example"
		idp := user()
		if mode != "postgresql" {
			if err := cassandra.Query(`INSERT INTO domain_registration (domain,domain_redirect,team_invite,idp_id) VALUES (?,?,?,?)`, domain, int32(3), int32(1), idp).WithContext(ctx).Consistency(gocql.LocalQuorum).Exec(); err != nil {
				t.Fatal(err)
			}
		} else {
			if _, err := postgres.Exec(ctx, `INSERT INTO domain_registration (domain,domain_redirect,team_invite,idp_id) VALUES ($1,3,1,$2::uuid)`, domain, idp.String()); err != nil {
				t.Fatal(err)
			}
		}
		store := domainRegistrationStore{databases: domainDB, mode: mode}
		record, err := store.lookup(ctx, strings.ToUpper(domain))
		if err != nil || record == nil || record.idp == nil || *record.idp != idp {
			t.Fatalf("%s domain read: %v", mode, err)
		}
		if err := guardRegistrationDomain(ctx, "synthetic@"+domain, store.lookup); !errors.Is(err, errDomainRegistrationSSO) {
			t.Fatal("SSO registration guard failed", mode, err)
		}
		if mode != "postgresql" {
			if err := cassandra.Query(`UPDATE domain_registration SET idp_id=null WHERE domain=?`, domain).WithContext(ctx).Consistency(gocql.LocalQuorum).Exec(); err != nil {
				t.Fatal(err)
			}
		} else {
			if _, err := postgres.Exec(ctx, `UPDATE domain_registration SET idp_id=null WHERE domain=$1`, domain); err != nil {
				t.Fatal(err)
			}
		}
		logged := 0
		store.invalidRecord = func() { logged++ }
		if record, err := store.lookup(ctx, domain); err != nil || record != nil || logged != 1 {
			t.Fatal("inconsistent native domain registration not ignored", mode, err)
		}
		if mode != "postgresql" {
			if err := cassandra.Query(`UPDATE domain_registration SET domain_redirect=? WHERE domain=?`, int32(9), domain).WithContext(ctx).Consistency(gocql.LocalQuorum).Exec(); err != nil {
				t.Fatal(err)
			}
		} else {
			if _, err := postgres.Exec(ctx, `UPDATE domain_registration SET domain_redirect=9 WHERE domain=$1`, domain); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := store.lookup(ctx, domain); err == nil {
			t.Fatal("unknown stored domain tag ignored", mode)
		}
	}
	pgDomain := "pg.only.synthetic.example"
	idp := user()
	if _, err := postgres.Exec(ctx, `INSERT INTO domain_registration (domain,domain_redirect,idp_id) VALUES ($1,3,$2::uuid)`, pgDomain, idp.String()); err != nil {
		t.Fatal(err)
	}
	migratingDomains := domainRegistrationStore{databases: domainDB, mode: "migration-to-postgresql"}
	if record, err := migratingDomains.lookup(ctx, pgDomain); err != nil || record != nil {
		t.Fatal("PG-only domain incorrectly used in migration", err)
	}
	if err := cassandra.Query(`INSERT INTO domain_registration (domain) VALUES (?)`, pgDomain).WithContext(ctx).Consistency(gocql.LocalQuorum).Exec(); err != nil {
		t.Fatal(err)
	}
	if err := guardRegistrationDomain(ctx, "synthetic@"+pgDomain, migratingDomains.lookup); err != nil {
		t.Fatal("native legacy missing domain tags rejected", err)
	}
	pgDomains := domainRegistrationStore{databases: domainDB, mode: "postgresql"}
	if err := guardRegistrationDomain(ctx, "synthetic@"+pgDomain, pgDomains.lookup); !errors.Is(err, errDomainRegistrationSSO) {
		t.Fatal("PG domain selector failed", err)
	}

	// Native registration construction and stale-owner verification reuse the
	// actual user/key/blocklist stores, with independently selected backends.
	for mode, store := range stores {
		id := user()
		email := "synthetic-registration-" + id.String() + "@example.com"
		created, err := (newRegistrationUser{Name: "Synthetic registration", UUID: &id, Email: &email}).build(nil, nil, &password, registrationLocale{Language: "en"}, 30, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if err := store.create(ctx, created, nil, nil); err != nil {
			t.Fatal(mode, "registration persistence", err)
		}
		disabledFeature, enabledFeature := int32(0), int32(1)
		for _, value := range []*int32{nil, &enabledFeature, &disabledFeature} {
			if err := store.updateConferenceCalling(ctx, id, value); err != nil {
				t.Fatal(mode, "registration feature write", err)
			}
			actual, err := store.conferenceCalling(ctx, id)
			if err != nil || (actual == nil) != (value == nil) || actual != nil && *actual != *value {
				t.Fatal(mode, "registration feature state", err)
			}
		}
		if err := store.updateConferenceCalling(ctx, id, nil); err != nil {
			t.Fatal(err)
		}
		if actual, err := store.conferenceCalling(ctx, id); err != nil || actual != nil {
			t.Fatal(mode, "feature null clear", err)
		}
		if ok, err := store.claimEmailKey(ctx, email, id); err != nil || !ok {
			t.Fatal(mode, "registration owner", err)
		}
		block := blocklistStore{databases: store.userAuthStore, mode: mode}
		identity := registrationIdentity{owner: store.registrationEmailOwner, account: store.get, deleteKey: store.deleteEmailKey, blacklisted: block.exists}
		if err := identity.verify(ctx, email); err != nil {
			t.Fatal(mode, "unactivated native account rejected", err)
		}
		if err := store.setEmailIdentity(ctx, id, &email, true); err != nil {
			t.Fatal(err)
		}
		if err := identity.verify(ctx, email); !errors.Is(err, errActivationKeyExists) {
			t.Fatal(mode, "activated identity not protected", err)
		}
		if err := store.updateAccountStatus(ctx, id, 2); err != nil {
			t.Fatal(err)
		}
		if err := identity.verify(ctx, email); err != nil {
			t.Fatal(mode, "deleted registration key repair failed", err)
		}
		if owner, err := store.registrationEmailOwner(ctx, email); err != nil || owner != nil {
			t.Fatal(mode, "deleted owner key remains", err)
		}
		if err := block.write(ctx, email, false); err != nil {
			t.Fatal(err)
		}
		if err := identity.verify(ctx, email); !errors.Is(err, errRegistrationBlacklisted) {
			t.Fatal(mode, "registration blacklist not enforced", err)
		}
		ephemeralID := user()
		now := time.Now()
		ephemeral, err := (newRegistrationUser{Name: "Synthetic ephemeral", UUID: &ephemeralID}).build(nil, nil, nil, registrationLocale{Language: "en"}, 30, now)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.create(ctx, ephemeral, nil, nil); err != nil {
			t.Fatal(err)
		}
		persisted, err := store.get(ctx, ephemeralID)
		if err != nil || persisted == nil || persisted.Status == nil || *persisted.Status != 3 || persisted.Expires == nil || !persisted.Expires.Equal(now.Add(30*time.Second).UTC().Truncate(time.Millisecond)) {
			t.Fatal(mode, "ephemeral native state mismatch", err)
		}
	}
}
