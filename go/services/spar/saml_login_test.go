// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type loginBoundary struct {
	team      gocql.UUID
	mappings  map[string]gocql.UUID
	accounts  map[gocql.UUID]*brigAccount
	tokens    []tokenInfo
	idps      []idpConfig
	failureAt string
	events    []string
}

func (b *loginBoundary) effect(name string) error {
	b.events = append(b.events, name)
	if b.failureAt == name {
		return errors.New("synthetic failure")
	}
	return nil
}
func (b *loginBoundary) samlUser(_ context.Context, r samlUserRef) (*gocql.UUID, error) {
	if err := b.effect("lookup:" + r.Issuer); err != nil {
		return nil, err
	}
	v, ok := b.mappings[r.Issuer]
	if !ok {
		return nil, nil
	}
	return &v, nil
}
func (b *loginBoundary) insertSAMLUser(_ context.Context, r samlUserRef, u gocql.UUID) error {
	if err := b.effect("insert:" + r.Issuer); err != nil {
		return err
	}
	b.mappings[r.Issuer] = u
	return nil
}
func (b *loginBoundary) deleteSAMLUser(_ context.Context, _ gocql.UUID, r samlUserRef) error {
	if err := b.effect("delete:" + r.Issuer); err != nil {
		return err
	}
	delete(b.mappings, r.Issuer)
	return nil
}
func (b *loginBoundary) teamTokens(context.Context, gocql.UUID) ([]tokenInfo, error) {
	return b.tokens, b.effect("tokens")
}
func (b *loginBoundary) idpsByTeam(context.Context, gocql.UUID) ([]idpConfig, error) {
	return b.idps, b.effect("team-idps")
}
func (b *loginBoundary) account(_ context.Context, u gocql.UUID, _ string, pending bool) (*brigAccount, error) {
	name := "account-active"
	if pending {
		name = "account-pending"
	}
	if err := b.effect(name); err != nil {
		return nil, err
	}
	return b.accounts[u], nil
}
func (b *loginBoundary) createSAMLAccount(_ context.Context, u, team gocql.UUID, _ samlUserRef, name, managed, role, _ string) error {
	if name != "synthetic@example.test" || managed != "wire" || role != "member" || team != b.team {
		return errors.New("unexpected account creation")
	}
	if err := b.effect("create"); err != nil {
		return err
	}
	b.accounts[u] = &brigAccount{ID: u, Team: &team}
	return nil
}
func (b *loginBoundary) setSAMLIdentity(context.Context, gocql.UUID, samlUserRef, string) error {
	return b.effect("set-identity")
}
func (b *loginBoundary) samlEmailValidation(context.Context, gocql.UUID, string) (bool, error) {
	return true, b.effect("email-feature")
}
func (b *loginBoundary) updateSAMLEmail(_ context.Context, _ gocql.UUID, email, _ string, validate bool) error {
	if email != "synthetic@example.test" || !validate {
		return errors.New("unexpected email activation")
	}
	return b.effect("email-update")
}
func (b *loginBoundary) ssoLogin(context.Context, gocql.UUID, *string, string) (string, error) {
	if err := b.effect("session"); err != nil {
		return "", err
	}
	return "zuid=synthetic", nil
}
func loginFixture(t *testing.T) (*loginBoundary, idpConfig, samlUserRef, gocql.UUID) {
	t.Helper()
	team, err := gocql.ParseUUID("00000000-0000-4000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	user, err := gocql.ParseUUID("00000000-0000-4000-8000-000000000002")
	if err != nil {
		t.Fatal(err)
	}
	return &loginBoundary{team: team, mappings: map[string]gocql.UUID{}, accounts: map[gocql.UUID]*brigAccount{}}, idpConfig{Extra: idpExtra{Team: team, OldIssuers: []string{"https://old.example/idp"}}}, samlUserRef{Issuer: "https://new.example/idp", Subject: samlNameID{Format: nameID11 + "emailAddress", Value: "synthetic@example.test"}}, user
}
func TestSAMLProvisioningFailureBoundaries(t *testing.T) {
	for _, failure := range []string{"", "create", "insert:https://new.example/idp", "email-update", "session"} {
		t.Run("failure="+failure, func(t *testing.T) {
			b, idp, ref, _ := loginFixture(t)
			b.failureAt = failure
			uid, cookie, err := samlLoginUser(context.Background(), Config{}, b, b, idp, ref, nil, nil, "synthetic-request")
			want := []string{"lookup:https://new.example/idp", "lookup:https://old.example/idp", "tokens", "create", "insert:https://new.example/idp", "account-active", "email-feature", "email-update", "session"}
			if failure != "" {
				for i, event := range want {
					if event == failure {
						want = want[:i+1]
						break
					}
				}
				if err == nil || cookie != "" || uid != (gocql.UUID{}) {
					t.Fatal("failed provisioning granted session")
				}
			} else if err != nil || cookie != "zuid=synthetic" || uid == (gocql.UUID{}) {
				t.Fatal("provisioning did not use real-session boundary", err)
			}
			if !reflect.DeepEqual(b.events, want) {
				t.Fatal("native provisioning order", b.events, want)
			}
		})
	}
}
func TestSAMLProvisioningGuardsAndWrongTeam(t *testing.T) {
	for _, kind := range []string{"replaced", "scim", "wrong-team"} {
		t.Run(kind, func(t *testing.T) {
			b, idp, ref, user := loginFixture(t)
			want := []string{"lookup:https://new.example/idp", "lookup:https://old.example/idp"}
			label := ""
			switch kind {
			case "replaced":
				idp.Extra.ReplacedBy = &user
				label = "cannont-provision-on-replaced-idp"
			case "scim":
				b.tokens = []tokenInfo{{}}
				want = append(want, "tokens")
				label = "not-found"
			case "wrong-team":
				b.mappings[ref.Issuer] = user
				b.accounts[user] = &brigAccount{ID: user, Team: &user}
				want = []string{"lookup:https://new.example/idp", "account-pending"}
				label = "bad-team"
			}
			_, cookie, err := samlLoginUser(context.Background(), Config{}, b, b, idp, ref, nil, nil, "synthetic-request")
			var api *apiError
			if !errors.As(err, &api) || api.Label != label || cookie != "" || !reflect.DeepEqual(b.events, want) {
				t.Fatal("provisioning/team guard failed", err, b.events)
			}
		})
	}
}
func TestSAMLOldIssuerMigrationPreservesFailureState(t *testing.T) {
	for _, failure := range []string{"", "set-identity"} {
		t.Run("failure="+failure, func(t *testing.T) {
			b, idp, ref, user := loginFixture(t)
			old := idp.Extra.OldIssuers[0]
			b.mappings[old] = user
			b.accounts[user] = &brigAccount{ID: user, Team: &b.team}
			b.failureAt = failure
			uid, cookie, err := samlLoginUser(context.Background(), Config{}, b, b, idp, ref, nil, nil, "synthetic-request")
			want := []string{"lookup:https://new.example/idp", "lookup:https://old.example/idp", "account-pending", "insert:https://new.example/idp", "set-identity"}
			if failure == "" {
				want = append(want, "delete:https://old.example/idp", "session")
				if err != nil || uid != user || cookie != "zuid=synthetic" {
					t.Fatal("migration failed", err)
				}
				if _, ok := b.mappings[old]; ok {
					t.Fatal("old mapping retained after success")
				}
			} else if err == nil || cookie != "" || b.mappings[old] != user {
				t.Fatal("failed identity update consumed old mapping or granted session")
			}
			if b.mappings[ref.Issuer] != user || !reflect.DeepEqual(b.events, want) {
				t.Fatal("native migration effects changed", b.events)
			}
		})
	}
}
