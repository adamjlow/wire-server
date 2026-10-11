// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"crypto/ed25519"
	"errors"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type memoryLoginBudget struct {
	value         *loginBudget
	reads, writes int
	key           string
	err           error
}

func (b *memoryLoginBudget) lookup(_ context.Context, key string) (*loginBudget, error) {
	b.reads++
	b.key = key
	return b.value, b.err
}
func (b *memoryLoginBudget) insert(_ context.Context, key string, value loginBudget) error {
	b.writes++
	b.key = key
	b.value = &value
	return b.err
}

func testLoginService(t *testing.T) (loginService, *memoryCookieStore, *memoryLoginBudget, gocql.UUID) {
	t.Helper()
	m, cookies, user := testSessionManager()
	budgets := &memoryLoginBudget{}
	email := "synthetic-login@example.com"
	service := loginService{
		resolve:      func(context.Context, loginIdentifier) (*gocql.UUID, error) { return &user, nil },
		pending:      func(context.Context, string) (bool, error) { return false, nil },
		authenticate: func(context.Context, gocql.UUID, string) error { return nil },
		account: func(context.Context, gocql.UUID) (*storedUser, error) {
			return &storedUser{ID: user, Activated: true, Email: &email}, nil
		},
		verificationEnabled: func(context.Context, string, string) (bool, error) { return false, nil },
		verifyCode: func(context.Context, string, string, string) (bool, error) {
			t.Fatal("unexpected verification call")
			return false, nil
		},
		sessions: m, budgets: budgets, limit: &failedLoginLimit{Timeout: 30, Retries: 2},
		now: func() time.Time { return time.Unix(1000, 0) },
	}
	return service, cookies, budgets, user
}

func TestLoginFailedBudgetBlocksBeforePasswordAndDoesNotIssue(t *testing.T) {
	s, cookies, budgets, user := testLoginService(t)
	attempts := 0
	s.authenticate = func(context.Context, gocql.UUID, string) error { attempts++; return authInvalidCredentials }
	for i := 0; i < 2; i++ {
		if _, err := s.login(context.Background(), loginRequest{}, "synthetic"); !errors.Is(err, loginFailed) {
			t.Fatal(err)
		}
	}
	var blocked loginBlocked
	if _, err := s.login(context.Background(), loginRequest{}, "synthetic"); !errors.As(err, &blocked) || blocked.seconds != 30 {
		t.Fatal("exhausted login did not preserve remaining timeout", err)
	}
	if attempts != 2 || budgets.writes != 2 || budgets.key != "login#"+user.String() || cookies.writes != 0 {
		t.Fatal("retry budget allowed password work or session issuance", attempts, budgets, cookies.writes)
	}
}

func TestLoginSuspendedAndUnresolvedAccountsDoNotConsumeRetries(t *testing.T) {
	s, cookies, budgets, _ := testLoginService(t)
	s.authenticate = func(context.Context, gocql.UUID, string) error { return authSuspended }
	if _, err := s.login(context.Background(), loginRequest{}, ""); !errors.Is(err, authSuspended) || budgets.writes != 0 {
		t.Fatal(err)
	}
	s.resolve = func(context.Context, loginIdentifier) (*gocql.UUID, error) { return nil, nil }
	email := "synthetic-pending@example.com"
	s.pending = func(context.Context, string) (bool, error) { return true, nil }
	if _, err := s.login(context.Background(), loginRequest{identifier: loginIdentifier{email: &email}}, ""); !errors.Is(err, loginPending) {
		t.Fatal(err)
	}
	s.pending = func(context.Context, string) (bool, error) {
		t.Fatal("handle queried email activation")
		return false, nil
	}
	handle := "synthetic-handle"
	if _, err := s.login(context.Background(), loginRequest{identifier: loginIdentifier{handle: &handle}}, ""); !errors.Is(err, loginFailed) {
		t.Fatal(err)
	}
	if budgets.reads != 1 || budgets.writes != 0 || cookies.writes != 0 {
		t.Fatal("unresolved account consumed user budget")
	}
}

func TestLoginVerificationFailureConsumesBudgetBeforeSession(t *testing.T) {
	s, cookies, budgets, user := testLoginService(t)
	team := gocql.UUID{1}
	email := "synthetic-verification@example.com"
	s.account = func(context.Context, gocql.UUID) (*storedUser, error) {
		return &storedUser{ID: user, Activated: true, Email: &email, Team: &team}, nil
	}
	s.verificationEnabled = func(_ context.Context, tid, rid string) (bool, error) {
		if tid != team.String() || rid != "synthetic-request" {
			t.Fatal("feature request lost identity")
		}
		return true, nil
	}
	if _, err := s.login(context.Background(), loginRequest{}, "synthetic-request"); !errors.Is(err, loginCodeRequired) {
		t.Fatal(err)
	}
	code := "123456"
	s.verifyCode = func(_ context.Context, e, action, value string) (bool, error) {
		if e != email || action != "login" || value != code {
			t.Fatal("incorrect verification scope")
		}
		return false, nil
	}
	if _, err := s.login(context.Background(), loginRequest{code: &code}, "synthetic-request"); !errors.Is(err, loginCodeInvalid) {
		t.Fatal(err)
	}
	if budgets.writes != 2 || cookies.writes != 0 {
		t.Fatal("verification bypassed retry control")
	}
}

func TestLoginSuccessPreservesBudgetAndIssuesVerifiableSession(t *testing.T) {
	for _, persistent := range []bool{false, true} {
		s, cookies, budgets, user := testLoginService(t)
		budgets.value = &loginBudget{seconds: 12, remaining: 1}
		access, err := s.login(context.Background(), loginRequest{persistent: persistent}, "")
		if err != nil {
			t.Fatal(err)
		}
		if access.cookie == nil || len(cookies.cookies) != 1 || access.token.User != user.String() || budgets.writes != 0 || budgets.value.remaining != 1 {
			t.Fatal("login changed budget or omitted session")
		}
		keys := []ed25519.PublicKey{s.sessions.issuer.key.Public().(ed25519.PublicKey)}
		if err := access.token.Verify(keys, s.now()); err != nil {
			t.Fatal("access signature rejected", err)
		}
		if err := access.cookie.token.Verify(keys, s.now()); err != nil {
			t.Fatal("cookie signature rejected", err)
		}
		expected := int32(0)
		if persistent {
			expected = 1
		}
		if access.cookie.record.Type != expected || access.cookie.token.Session == persistent {
			t.Fatal("wrong cookie kind")
		}
		encoded, err := access.token.Encode()
		if err != nil || encoded == "" {
			t.Fatal("unsigned access token", err)
		}
	}
}

func TestLoginStorageFailureCannotBeDisguisedAsBadCredentials(t *testing.T) {
	s, cookies, budgets, _ := testLoginService(t)
	failure := errors.New("synthetic budget failure")
	budgets.err = failure
	s.authenticate = func(context.Context, gocql.UUID, string) error {
		t.Fatal("password evaluated despite store failure")
		return nil
	}
	if _, err := s.login(context.Background(), loginRequest{}, ""); !errors.Is(err, failure) || cookies.writes != 0 {
		t.Fatal(err)
	}
}

func TestLoginEmailKeyTrustedDomainCaseRule(t *testing.T) {
	for _, example := range []struct{ input, want string }{
		{"Alice+suffix@example.com", "alice@example.com"},
		{"Alice+suffix@wire.com", "alice+suffix@wire.com"},
		{"Alice+suffix@Wire.com", "alice@wire.com"},
	} {
		key, err := emailUniqueKey(example.input)
		if err != nil || key != example.want {
			t.Fatal(example.input, key, err)
		}
	}
}
