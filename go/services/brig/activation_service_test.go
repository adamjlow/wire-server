// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type memoryActivation struct {
	record   *activationRecord
	user     *storedUser
	steps    []string
	claim    bool
	fail     string
	selected gocql.UUID
}

func (m *memoryActivation) step(name string) error {
	m.steps = append(m.steps, name)
	if name == m.fail {
		return errors.New("synthetic activation storage failure")
	}
	return nil
}
func (m *memoryActivation) verify(context.Context, string, string) (*activationRecord, error) {
	return m.record, m.step("verify")
}
func (m *memoryActivation) account(_ context.Context, user gocql.UUID) (*storedUser, error) {
	m.selected = user
	return m.user, m.step("account")
}
func (m *memoryActivation) claimEmailKey(context.Context, string, gocql.UUID) (bool, error) {
	return m.claim, m.step("claim")
}
func (m *memoryActivation) setEmailIdentity(_ context.Context, _ gocql.UUID, email *string, activate bool) error {
	if err := m.step("email"); err != nil {
		return err
	}
	m.user.Email = email
	if activate {
		m.user.Activated = true
	}
	return nil
}
func (m *memoryActivation) clearUnvalidatedEmail(context.Context, gocql.UUID) error {
	if err := m.step("clear-unvalidated"); err != nil {
		return err
	}
	m.user.EmailUnvalidated = nil
	return nil
}
func (m *memoryActivation) deleteEmailKey(context.Context, string, *gocql.UUID) error {
	return m.step("delete-old-key")
}
func (m *memoryActivation) reset(context.Context, gocql.UUID) error { return m.step("reset") }
func activationFixture() (activationService, *memoryActivation) {
	user := gocql.UUID{1}
	m := &memoryActivation{record: &activationRecord{email: "synthetic@example.com", user: &user}, user: &storedUser{ID: user}, claim: true}
	return activationService{codes: m, account: m.account, identities: m, deletePasswordReset: m.reset}, m
}

func TestActivationFirstClaimsBeforeMakingIdentityVisible(t *testing.T) {
	s, m := activationFixture()
	unvalidated := "synthetic-unvalidated@example.com"
	m.user.EmailUnvalidated = &unvalidated
	result, err := s.activateKey(context.Background(), "synthetic-key", "123456", nil)
	if err != nil || result == nil || !result.first || result.account == nil || !result.account.Activated || result.account.Email == nil || *result.account.Email != "synthetic@example.com" {
		t.Fatal("activation omitted visible identity", err)
	}
	if !reflect.DeepEqual(m.steps, []string{"verify", "account", "claim", "email"}) || m.user.EmailUnvalidated == nil {
		t.Fatal("first activation changed native side effects", m.steps)
	}
	s, m = activationFixture()
	m.claim = false
	if _, err := s.activateKey(context.Background(), "synthetic-key", "123456", nil); !errors.Is(err, errActivationKeyExists) || m.user.Activated {
		t.Fatal("claimed identity activated", err)
	}
}

func TestActivationRepeatedLinkDoesNotMutateOrInvalidateReset(t *testing.T) {
	s, m := activationFixture()
	m.user.Activated = true
	m.user.Email = &m.record.email
	result, err := s.activateKey(context.Background(), "synthetic-key", "123456", nil)
	if err != nil || result != nil || !reflect.DeepEqual(m.steps, []string{"verify", "account"}) {
		t.Fatal("repeat link mutated account", result, err, m.steps)
	}
}

func TestActivationEmailAliasOnlyUpdatesProfile(t *testing.T) {
	s, m := activationFixture()
	old := "Synthetic+old@example.com"
	m.user.Activated = true
	m.user.Email = &old
	m.record.email = "synthetic+new@example.com"
	result, err := s.activateKey(context.Background(), "synthetic-key", "123456", nil)
	if err != nil || result == nil || result.first || m.user.Email == nil || *m.user.Email != m.record.email || !reflect.DeepEqual(m.steps, []string{"verify", "account", "email", "clear-unvalidated"}) {
		t.Fatal("same-key email reset or reclaimed identity", err, m.steps)
	}
}

func TestActivationNewKeyInvalidatesResetBeforeClaimAndReleasesOldAfterUpdate(t *testing.T) {
	s, m := activationFixture()
	old := "synthetic-old@example.com"
	m.user.Activated = true
	m.user.Email = &old
	result, err := s.activateKey(context.Background(), "synthetic-key", "123456", nil)
	if err != nil || result == nil || result.first || !reflect.DeepEqual(m.steps, []string{"verify", "account", "reset", "claim", "email", "clear-unvalidated", "delete-old-key"}) {
		t.Fatal("email replacement changed security ordering", err, m.steps)
	}
	s, m = activationFixture()
	m.user.Activated = true
	m.user.Email = &old
	m.claim = false
	if _, err := s.activateKey(context.Background(), "synthetic-key", "123456", nil); !errors.Is(err, errActivationKeyExists) || !reflect.DeepEqual(m.steps, []string{"verify", "account", "reset", "claim"}) || *m.user.Email != old {
		t.Fatal("failed claim preserved reset or changed email", err, m.steps)
	}
}

func TestActivationRejectsInactiveAndMissingAccountsWithoutMutation(t *testing.T) {
	for _, status := range []int32{1, 2, 3, 4} {
		s, m := activationFixture()
		m.user.Status = &status
		if _, err := s.activateKey(context.Background(), "synthetic-key", "123456", nil); !errors.Is(err, errInvalidActivationCode) || len(m.steps) != 2 {
			t.Fatal("non-active account activated", status, err, m.steps)
		}
	}
	s, m := activationFixture()
	m.user = nil
	if _, err := s.activateKey(context.Background(), "synthetic-key", "123456", nil); !errors.Is(err, errActivationWrongUser) {
		t.Fatal(err)
	}
	s, m = activationFixture()
	m.record.user = nil
	if _, err := s.activateKey(context.Background(), "synthetic-key", "123456", nil); !errors.Is(err, errActivationWrongUser) || len(m.steps) != 1 {
		t.Fatal("missing user was invented", err, m.steps)
	}
	override := gocql.UUID{2}
	if _, err := s.activateKey(context.Background(), "synthetic-key", "123456", &override); err != nil || m.selected != override {
		t.Fatal("native explicit internal user override changed", err)
	}
}

func TestActivationStoreFailuresCannotReleaseOldIdentity(t *testing.T) {
	for _, failure := range []string{"verify", "account", "reset", "claim", "email", "clear-unvalidated"} {
		s, m := activationFixture()
		old := "synthetic-old@example.com"
		m.user.Activated = true
		m.user.Email = &old
		m.fail = failure
		if _, err := s.activateKey(context.Background(), "synthetic-key", "123456", nil); err == nil {
			t.Fatal("storage failure hidden", failure)
		}
		for _, step := range m.steps {
			if step == "delete-old-key" {
				t.Fatal("released identity after failed update", failure)
			}
		}
	}
}
