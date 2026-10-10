// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type memoryAuthenticationStore struct {
	record     *userAuthentication
	writes     int
	writeError error
}

func (s *memoryAuthenticationStore) authentication(context.Context, gocql.UUID) (*userAuthentication, error) {
	return s.record, nil
}
func (s *memoryAuthenticationStore) setPassword(_ context.Context, _ gocql.UUID, value string) error {
	if s.writeError != nil {
		return s.writeError
	}
	s.writes++
	s.record.Password = &value
	return nil
}

func testPasswordService(t *testing.T, burst uint64) *passwordService {
	t.Helper()
	l, err := newPasswordRateLimiter(passwordRateConfig{User: tokenBucketConfig{burst, 60000000}, MaxKeys: 10})
	if err != nil {
		t.Fatal(err)
	}
	l.now = func() time.Time { return time.UnixMicro(1000) }
	p, err := newPasswordService(passwordPolicy{Algorithm: "argon2id", Memory: 128, Iterations: 1, Parallelism: 1}, l, 1)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAuthenticationStatusAndWrongPasswordNeverUpgrade(t *testing.T) {
	plain := "synthetic-password"
	encoded, err := newPasswordHash(plain, true, 128, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	for status, want := range map[int32]error{1: authSuspended, 2: authInvalidCredentials, 3: authEphemeral, 4: authPendingInvitation} {
		store := &memoryAuthenticationStore{record: &userAuthentication{Password: &encoded, Status: status}}
		a := authenticationService{store: store, passwords: testPasswordService(t, 0)}
		if err := a.authenticate(context.Background(), gocql.UUID{}, plain); !errors.Is(err, want) || len(a.passwords.rate.buckets) != 0 {
			t.Fatal("account status must reject before hashing", status, err)
		}
	}
	store := &memoryAuthenticationStore{record: &userAuthentication{Password: &encoded}}
	a := authenticationService{store: store, passwords: testPasswordService(t, 5)}
	if err := a.authenticate(context.Background(), gocql.UUID{}, "wrong-password"); !errors.Is(err, authInvalidCredentials) || store.writes != 0 {
		t.Fatal("wrong password changed stored hash", err)
	}
	if err := a.authenticate(context.Background(), gocql.UUID{}, plain); err != nil || store.writes != 0 {
		t.Fatal("current Argon password should not upgrade", err)
	}
}

func TestAuthenticationUpgradeThrottlingAndWriteFailure(t *testing.T) {
	plain := "synthetic-password"
	encoded, err := newPasswordHash(plain, false, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, burst := range []uint64{1, 2} {
		stored := encoded
		store := &memoryAuthenticationStore{record: &userAuthentication{Password: &stored}}
		a := authenticationService{store: store, passwords: testPasswordService(t, burst)}
		if err := a.authenticate(context.Background(), gocql.UUID{}, plain); err != nil {
			t.Fatal("upgrade throttling must not fail valid authentication", err)
		}
		if burst == 1 && (store.writes != 0 || *store.record.Password != encoded) {
			t.Fatal("throttled upgrade changed stored password")
		}
		if burst == 2 {
			if store.writes != 1 || !strings.HasPrefix(*store.record.Password, "$argon2id$v=19$") {
				t.Fatal("successful upgrade did not persist Argon")
			}
			if matched, _, err := verifyPasswordHash(*store.record.Password, plain, true); err != nil || !matched {
				t.Fatal("upgraded password cannot authenticate", err)
			}
		}
	}
	writeError := errors.New("synthetic storage failure")
	store := &memoryAuthenticationStore{record: &userAuthentication{Password: &encoded}, writeError: writeError}
	a := authenticationService{store: store, passwords: testPasswordService(t, 2)}
	if err := a.authenticate(context.Background(), gocql.UUID{}, plain); !errors.Is(err, writeError) {
		t.Fatal("storage errors must not be swallowed as upgrade throttling", err)
	}
}

func TestReauthenticationPasswordlessSAMLAndEphemeral(t *testing.T) {
	plain := "synthetic-password"
	encoded, err := newPasswordHash(plain, true, 128, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryAuthenticationStore{record: &userAuthentication{}}
	a := authenticationService{store: store, passwords: testPasswordService(t, 5), isSAML: func(context.Context, gocql.UUID) (bool, error) { return true, nil }}
	ctx, user := context.Background(), gocql.UUID{}
	if err := a.reauthenticate(ctx, user, nil); err != nil {
		t.Fatal(err)
	}
	if err := a.reauthenticate(ctx, user, &plain); !errors.Is(err, authInvalidCredentials) {
		t.Fatal("provided password requires stored password")
	}
	store.record = &userAuthentication{Password: &encoded, Status: 3}
	if err := a.reauthenticate(ctx, user, &plain); err != nil || store.writes != 0 {
		t.Fatal("ephemeral reauthentication is allowed without upgrades", err)
	}
	if err := a.reauthenticate(ctx, user, nil); err != nil {
		t.Fatal("SAML password omission should be allowed", err)
	}
	a.isSAML = func(context.Context, gocql.UUID) (bool, error) { return false, nil }
	if err := a.reauthenticate(ctx, user, nil); !errors.Is(err, authMissingPassword) {
		t.Fatal("non-SAML password omission must fail", err)
	}
}

func TestPasswordValidationAndCanceledQueue(t *testing.T) {
	if validatePlainPassword(strings.Repeat("é", 6), 6) != nil || validatePlainPassword(strings.Repeat("é", 1024), 6) != nil {
		t.Fatal("password length must count Unicode characters")
	}
	for _, plain := range []string{"short", strings.Repeat("x", 1025), string([]byte{0xff})} {
		if validatePlainPassword(plain, 6) == nil {
			t.Fatal("invalid plaintext accepted")
		}
	}
	p := testPasswordService(t, 5)
	p.slots <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.hash(ctx, passwordRateKey{kind: "user", id: "synthetic"}, "synthetic-password", 6); !errors.Is(err, context.Canceled) || len(p.rate.buckets) != 0 {
		t.Fatal("already-canceled work must not consume rate quota", err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := p.hash(ctx, passwordRateKey{kind: "user", id: "synthetic"}, "synthetic-password", 6); !errors.Is(err, context.DeadlineExceeded) || len(p.slots) != 1 {
		t.Fatal("queued canceled hashing must not acquire slot", err)
	}
	<-p.slots
}
