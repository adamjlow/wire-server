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
	bulkClients := clientBulkReads{clients: clients.list, ids: clients.ids}
	missingClientUser := user()
	fullMap, err := bulkClients.lookup(ctx, []gocql.UUID{clientOwner, missingClientUser}, true)
	if err != nil || len(fullMap) != 2 || len(fullMap[clientOwner.String()].([]map[string]any)) != len(listed) || len(fullMap[missingClientUser.String()].([]map[string]any)) != 0 {
		t.Fatal("actual bulk clients lost populated/empty user map", err)
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
	if clientIDs, err := clients.ids(ctx, clientOwner); err != nil || len(clientIDs) != len(listed) {
		t.Fatal("ID-only client query decoded unrelated orphan MLS keys", err)
	}
	if _, err := bulkClients.lookup(ctx, []gocql.UUID{clientOwner}, true); err == nil {
		t.Fatal("bulk full-client query hid malformed orphan MLS keys")
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
	if c.Settings.PasswordRate == nil || c.Settings.PasswordPolicy == nil {
		t.Fatal("native password configuration missing")
	}
	rate, err := newPasswordRateLimiter(*c.Settings.PasswordRate)
	if err != nil {
		t.Fatal(err)
	}
	passwords, err := newPasswordService(*c.Settings.PasswordPolicy, rate, 1)
	if err != nil {
		t.Fatal(err)
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
		// Exercise reset with actual configured KDF, native user/reset selectors
		// and shared Cassandra cookies; no mail callbacks or hash success stubs.
		if hash, err := store.hashedPassword(ctx, owner); err != nil || hash == nil || *hash != password {
			t.Fatal(mode, "reset password-only projection", err)
		}
		if hash, err := store.hashedPassword(ctx, user()); err != nil || hash != nil {
			t.Fatal(mode, "missing password-only projection", err)
		}
		if err := reset.insert(ctx, key, record, 30); err != nil {
			t.Fatal(err)
		}
		sessions := sessionStore{cassandra}
		at := time.Now().UTC().Truncate(time.Millisecond)
		if err := sessions.insert(ctx, owner, sessionCookie{ID: 1, Type: 1, Created: at, Expires: at.Add(time.Minute)}, nil); err != nil {
			t.Fatal(err)
		}
		resetFlow := passwordResetService{codes: reset, account: store.get, currentPassword: store.hashedPassword, setPassword: store.setPassword, deleteCookies: sessions.deleteAll, verifyPassword: passwords.verify, hashPassword: passwords.hash, now: time.Now}
		if err := resetFlow.complete(ctx, passwordResetRequest{key: &key, code: "wrong-synthetic-reset", password: "Synthetic-reset-store-password!"}); !errors.Is(err, errPasswordResetCode) {
			t.Fatal(mode, "actual reset wrong-code control", err)
		}
		if got, err := reset.lookup(ctx, key); err != nil || got == nil || got.retries == nil || *got.retries != 2 || got.timeout == nil || !got.timeout.Equal(timeout) {
			t.Fatal(mode, "actual reset retry/logical deadline", err)
		}
		if err := resetFlow.complete(ctx, passwordResetRequest{key: &key, code: code, password: "Synthetic-reset-store-password!"}); err != nil {
			t.Fatal(mode, "actual reset completion", err)
		}
		hash, err := store.hashedPassword(ctx, owner)
		if err != nil || hash == nil || *hash == password {
			t.Fatal(mode, "actual reset password not replaced", err)
		}
		matches, _, err := passwords.verify(ctx, passwordRateKey{kind: "user", id: owner.String()}, *hash, "Synthetic-reset-store-password!")
		if err != nil || !matches {
			t.Fatal(mode, "actual reset hash verification", err)
		}
		if got, err := reset.lookup(ctx, key); err != nil || got != nil {
			t.Fatal(mode, "completed reset retained code", err)
		}
		if cookies, err := sessions.list(ctx, owner); err != nil || len(cookies) != 0 {
			t.Fatal(mode, "completed reset retained cookies", err)
		}
		if mode == "migration-to-postgresql" {
			if got, err := (passwordResetStore{databases: reset.databases, mode: "postgresql"}).lookup(ctx, key); err != nil || got != nil {
				t.Fatal("completed reset retained PG mirror", err)
			}
		}
	}
	// Account password changes use the actual activated/password-only reads,
	// configured hashing and cookie store in all independently selected modes.
	for mode, store := range stores {
		actor := user()
		row := newRecord(actor)
		row.Password = nil
		row.Activated = true
		if err := store.create(ctx, row, nil, nil); err != nil {
			t.Fatal(err)
		}
		if active, err := store.isActivated(ctx, actor); err != nil || !active {
			t.Fatal(mode, "activated-only projection", err)
		}
		if active, err := store.isActivated(ctx, user()); err != nil || active {
			t.Fatal(mode, "missing activation projection", err)
		}
		session := sessionStore{cassandra}
		at := time.Now().UTC().Truncate(time.Millisecond)
		if err := session.insert(ctx, actor, sessionCookie{ID: 2, Type: 1, Created: at, Expires: at.Add(time.Minute)}, nil); err != nil {
			t.Fatal(err)
		}
		flow := passwordChangeService{activated: store.isActivated, current: store.hashedPassword, hash: passwords.hash, verify: passwords.verify, write: store.setPassword, revoke: session.deleteAll}
		first := "Synthetic-first-store-password!"
		if err := flow.change(ctx, actor, passwordChangeRequest{new: first}); err != nil {
			t.Fatal(mode, "first password write", err)
		}
		if cookies, err := session.list(ctx, actor); err != nil || len(cookies) != 1 {
			t.Fatal(mode, "first password revoked cookies", err)
		}
		if err := flow.change(ctx, actor, passwordChangeRequest{old: &first, new: "Synthetic-changed-store-password!"}); err != nil {
			t.Fatal(mode, "existing password change", err)
		}
		if cookies, err := session.list(ctx, actor); err != nil || len(cookies) != 0 {
			t.Fatal(mode, "changed password retained cookies", err)
		}
		hash, err := store.hashedPassword(ctx, actor)
		if err != nil || hash == nil {
			t.Fatal(mode, "changed password missing", err)
		}
		valid, _, err := passwords.verify(ctx, passwordRateKey{kind: "user", id: actor.String()}, *hash, "Synthetic-changed-store-password!")
		if err != nil || !valid {
			t.Fatal(mode, "changed password not verifiable", err)
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
		ssoID, teamID := user(), user()
		sso, err := decodeRegistrationSSO(`{"tenant":"https://issuer.example.com","subject":"synthetic"}`)
		if err != nil {
			t.Fatal(err)
		}
		protocols := int32(0)
		ssoAccount, err := (newRegistrationUser{Name: "Synthetic SSO", UUID: &ssoID, SSO: sso, Protocols: &protocols}).build(nil, &teamID, nil, registrationLocale{Language: "en"}, 30, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if err := store.create(ctx, ssoAccount, nil, nil); err != nil {
			t.Fatal(err)
		}
		if err := store.setEmailIdentity(ctx, ssoID, nil, true); err != nil {
			t.Fatal(err)
		}
		loaded, err := store.get(ctx, ssoID)
		if err != nil || loaded == nil || !loaded.Activated || loaded.Email != nil || loaded.Team == nil || *loaded.Team != teamID || len(loaded.SSO) == 0 {
			t.Fatal(mode, "native SSO activation persistence", err)
		}
		view, err := loaded.selfProfile("example.com", "en")
		if err != nil || len(view.SSO) == 0 || len(view.Protocols) != 1 || view.Protocols[0] != "proteus" {
			t.Fatal(mode, "native stored SSO projection", err)
		}
	}
	// AppStore is PostgreSQL in all user-storage modes; these callbacks must
	// return actual team-scoped rows, not an empty membership success fallback.
	appTeam, otherTeam, firstApp, secondApp, otherApp, creator := user(), user(), user(), user(), user(), user()
	for _, row := range []struct{ app, team gocql.UUID }{{firstApp, appTeam}, {secondApp, appTeam}, {otherApp, otherTeam}} {
		if _, err := postgres.Exec(ctx, `INSERT INTO apps(user_id,team_id,metadata,category,description,creator) VALUES($1::uuid,$2::uuid,'{}'::json,'other','',$3::uuid)`, row.app.String(), row.team.String(), creator.String()); err != nil {
			t.Fatal(err)
		}
	}
	for mode, store := range stores {
		ids, err := store.teamAppIDs(ctx, appTeam)
		if err != nil || len(ids) != 2 {
			t.Fatal(mode, "native app team lookup", ids, err)
		}
		seen := map[string]bool{}
		for _, id := range ids {
			seen[id] = true
		}
		if !seen[firstApp.String()] || !seen[secondApp.String()] || seen[otherApp.String()] {
			t.Fatal(mode, "app lookup leaked another team", ids)
		}
		empty, err := store.teamAppIDs(ctx, user())
		if err != nil || empty == nil || len(empty) != 0 {
			t.Fatal(mode, "missing app team must encode empty array", err)
		}
	}
	if _, err := postgres.Exec(ctx, `UPDATE apps SET description=$1 WHERE user_id=$2::uuid`, strings.Repeat("é", 301), firstApp.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := stores["cassandra"].teamAppIDs(ctx, appTeam); err == nil {
		t.Fatal("app IDs bypassed native full-row description validation")
	}
	for mode, store := range stores {
		first, second, missing := user(), user(), user()
		firstRow, secondRow := newRecord(first), newRecord(second)
		team := user()
		handle := "synthetic-batch-" + first.String()
		firstRow.Team, firstRow.Handle = &team, &handle
		if err := store.create(ctx, firstRow, nil, nil); err != nil {
			t.Fatal(err)
		}
		// Native account creation deliberately omits handles. Seed the later
		// handle update before asserting the read projection; do not change it.
		if mode == "cassandra" {
			if err := cassandra.Query(`UPDATE user SET handle=? WHERE id=?`, handle, first).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5}).Exec(); err != nil {
				t.Fatal(err)
			}
		} else if _, err := postgres.Exec(ctx, `UPDATE wire_user SET handle=$1 WHERE id=$2::uuid`, handle, first.String()); err != nil {
			t.Fatal(err)
		}
		if err := store.create(ctx, secondRow, nil, nil); err != nil {
			t.Fatal(err)
		}
		batch, err := store.getMany(ctx, []gocql.UUID{second, missing, first})
		if err != nil || len(batch) != 2 {
			t.Fatal(mode, "native multi-user read", len(batch), err)
		}
		seen := map[gocql.UUID]storedUser{}
		for _, row := range batch {
			seen[row.ID] = row
		}
		row, found := seen[first]
		if !found || row.Team == nil || *row.Team != team || row.Handle == nil || *row.Handle != handle || len(row.Assets) != 1 || row.Assets[0].Key != firstRow.Assets[0].Key {
			t.Fatal(mode, "native batch projection lost/swapped team, handle or assets")
		}
		if mode == "migration-to-postgresql" && (batch[0].ID != second || batch[1].ID != first) {
			t.Fatal("migration batch did not preserve request order")
		}
		if mode == "postgresql" && batch[0].ID.String() > batch[1].ID.String() {
			t.Fatal("PostgreSQL batch did not preserve native Map order")
		}
		if empty, err := store.getMany(ctx, nil); err != nil || empty == nil || len(empty) != 0 {
			t.Fatal(mode, "empty batch", err)
		}
	}
	shared, cassOnly := user(), user()
	if err := stores["cassandra"].create(ctx, newRecord(shared), nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := stores["cassandra"].create(ctx, newRecord(cassOnly), nil, nil); err != nil {
		t.Fatal(err)
	}
	pgRow := newRecord(shared)
	pgRow.Name = "Synthetic preferred PostgreSQL row"
	if err := stores["postgresql"].create(ctx, pgRow, nil, nil); err != nil {
		t.Fatal(err)
	}
	if batch, err := stores["migration-to-postgresql"].getMany(ctx, []gocql.UUID{cassOnly, shared}); err != nil || len(batch) != 2 || batch[0].ID != cassOnly || batch[1].Name != pgRow.Name {
		t.Fatal("native batch PostgreSQL priority/Cassandra fallback", err)
	}
	if _, err := postgres.Exec(ctx, `INSERT INTO deleted_user(id,created_at) VALUES($1::uuid,now())`, shared.String()); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"postgresql", "migration-to-postgresql"} {
		batch, err := stores[mode].getMany(ctx, []gocql.UUID{shared})
		if err != nil || len(batch) != 1 || batch[0].Status == nil || *batch[0].Status != 2 || batch[0].Name != "default" || batch[0].Email != nil || len(batch[0].Assets) != 0 {
			t.Fatal(mode, "native batch deletion tombstone failed", err)
		}
	}
	invites := invitationStore{cassandra}
	inviteEmail := "synthetic-invite-" + user().String() + "@example.com"
	liveTeam, staleTeam, liveID, staleID := user(), user(), user(), user()
	inviteCreated := time.Now().UTC().Truncate(time.Millisecond)
	// Native by-email lookup reads every reference and ignores missing main
	// rows. Optional role/name/creator remain valid legacy NULLs.
	for _, ref := range []struct{ team, id gocql.UUID }{{liveTeam, liveID}, {staleTeam, staleID}} {
		if err := cassandra.Query(`INSERT INTO team_invitation_email(email,team,invitation,code) VALUES(?,?,?,?)`, inviteEmail, ref.team, ref.id, "synthetic-code").WithContext(ctx).Consistency(gocql.LocalQuorum).Exec(); err != nil {
			t.Fatal(err)
		}
	}
	if err := cassandra.Query(`INSERT INTO team_invitation(team,id,created_at,email,code) VALUES(?,?,?,?,?)`, liveTeam, liveID, inviteCreated, inviteEmail, "synthetic-code").WithContext(ctx).Consistency(gocql.LocalQuorum).Exec(); err != nil {
		t.Fatal(err)
	}
	if found, err := invites.byEmail(ctx, inviteEmail); err != nil || len(found) != 1 || found[0].ID != liveID || found[0].Team != liveTeam || found[0].Role != nil || found[0].Name != nil || found[0].CreatedBy != nil || !found[0].CreatedAt.Equal(inviteCreated) {
		t.Fatal("native live/stale invitation references or optional decoding", err)
	}
	if found, err := invites.byEmail(ctx, "absent-"+inviteEmail); err != nil || found == nil || len(found) != 0 {
		t.Fatal("absent invitation lookup", err)
	}
	if err := cassandra.Query(`INSERT INTO team_invitation_info (code,team,id) VALUES (?,?,?)`, "synthetic_invitation", liveTeam, liveID).WithContext(ctx).Consistency(gocql.LocalQuorum).Exec(); err != nil {
		t.Fatal(err)
	}
	if got, err := invites.byCode(ctx, "synthetic_invitation"); err != nil || got == nil || got.ID != liveID || got.Team != liveTeam {
		t.Fatal("actual invitation code lookup", err)
	}
	if got, err := invites.byCode(ctx, "absent-synthetic-code"); err != nil || got != nil {
		t.Fatal("absent invitation code", err)
	}
	if err := cassandra.Query(`INSERT INTO team_invitation_info (code,team,id) VALUES (?,?,?)`, "synthetic_stale", staleTeam, staleID).WithContext(ctx).Consistency(gocql.LocalQuorum).Exec(); err != nil {
		t.Fatal(err)
	}
	if got, err := invites.byCode(ctx, "synthetic_stale"); err != nil || got != nil {
		t.Fatal("stale invitation code reference", err)
	}
	if err := cassandra.Query(`UPDATE team_invitation SET role=? WHERE team=? AND id=?`, int32(0), liveTeam, liveID).WithContext(ctx).Consistency(gocql.LocalQuorum).Exec(); err != nil {
		t.Fatal(err)
	}
	if _, err := invites.byEmail(ctx, inviteEmail); err == nil {
		t.Fatal("unknown stored invitation role accepted")
	}
	if err := cassandra.Query(`UPDATE team_invitation SET role=?,created_at=null WHERE team=? AND id=?`, int32(3), liveTeam, liveID).WithContext(ctx).Consistency(gocql.LocalQuorum).Exec(); err != nil {
		t.Fatal(err)
	}
	if _, err := invites.byEmail(ctx, inviteEmail); err == nil {
		t.Fatal("missing required invitation timestamp accepted")
	}
	// Deletion must still work with this unrelated malformed timestamp.
	if err := invites.delete(ctx, liveTeam, liveID); err != nil {
		t.Fatal("invitation deletion decoded unrelated metadata", err)
	}
	if got, err := invites.byCode(ctx, "synthetic_invitation"); err != nil || got != nil {
		t.Fatal("deleted code reference remains", err)
	}
	var remaining int
	if err := cassandra.Query(`SELECT count(*) FROM team_invitation_email WHERE email=? AND team=?`, inviteEmail, liveTeam).WithContext(ctx).Consistency(gocql.LocalQuorum).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatal("deleted email/team reference remains", err)
	}
	if err := cassandra.Query(`SELECT count(*) FROM team_invitation WHERE team=? AND id=?`, liveTeam, liveID).WithContext(ctx).Consistency(gocql.LocalQuorum).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatal("deleted main invitation remains", err)
	}
	if err := invites.delete(ctx, liveTeam, liveID); err != nil {
		t.Fatal("absent invitation deletion", err)
	}
	pendingID := user()
	if err := cassandra.Query(`INSERT INTO users_pending_activation (user,expires_at) VALUES (?,?)`, pendingID, time.Now().UTC()).WithContext(ctx).Consistency(gocql.LocalQuorum).Exec(); err != nil {
		t.Fatal(err)
	}
	if err := invites.removePending(ctx, pendingID); err != nil {
		t.Fatal("actual pending activation cleanup", err)
	}
	if err := cassandra.Query(`SELECT count(*) FROM users_pending_activation WHERE user=?`, pendingID).WithContext(ctx).Consistency(gocql.LocalQuorum).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatal("pending activation row remains", err)
	}
	qualifyAccountDeletionStores(t, ctx, stores, newRecord)
	qualifyDeletionGroupStores(t, ctx, stores)
	qualifyRemoteDeletionStores(t, ctx, connectionStore{cassandra: cassandra, domain: "example.com"})

}
