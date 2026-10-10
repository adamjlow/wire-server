// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"testing"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type cleanupBoundary struct {
	internalCleanupStore
	events []string
	idps   []idpConfig
	fail   bool
}

func (s *cleanupBoundary) deleteTeamTokens(context.Context, gocql.UUID) error {
	s.events = append(s.events, "tokens")
	if s.fail {
		return errors.New("synthetic failure")
	}
	return nil
}
func (s *cleanupBoundary) idpsByTeam(context.Context, gocql.UUID) ([]idpConfig, error) {
	s.events = append(s.events, "idps")
	return s.idps, nil
}
func (s *cleanupBoundary) deleteSAMLUsersByIssuer(_ context.Context, issuer string) error {
	s.events = append(s.events, "issuer:"+issuer)
	return nil
}
func (s *cleanupBoundary) deleteIdPConfig(_ context.Context, info idpConfig) error {
	s.events = append(s.events, "config:"+info.Metadata.Issuer)
	return nil
}
func (s *cleanupBoundary) deleteSCIMExternal(_ context.Context, _ gocql.UUID, external string) error {
	s.events = append(s.events, "external:"+external)
	return nil
}
func (s *cleanupBoundary) deleteSCIMMeta(context.Context, gocql.UUID) error {
	s.events = append(s.events, "meta")
	return nil
}

func TestInternalTeamCleanupOrderingAndFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		s := &cleanupBoundary{fail: fail, idps: []idpConfig{{Metadata: idpMetadata{Issuer: "first"}}, {Metadata: idpMetadata{Issuer: "second"}}}}
		err := cleanupTeam(context.Background(), s, scimTestID(t, "000000000001"))
		want := []string{"tokens", "idps", "issuer:first", "config:first", "issuer:second", "config:second"}
		if fail {
			want = []string{"tokens"}
		}
		if (err != nil) != fail || !reflect.DeepEqual(s.events, want) {
			t.Fatal("cleanup effects", s.events, err)
		}
	}
}

func TestInternalPendingCleanupGuardsAndDoesNotDeleteBrig(t *testing.T) {
	team, uid, foreign := scimTestID(t, "000000000001"), scimTestID(t, "000000000002"), scimTestID(t, "000000000003")
	for _, scenario := range []string{"pending", "no-identity", "active", "foreign", "wire", "absent"} {
		t.Run(scenario, func(t *testing.T) {
			s := &cleanupBoundary{}
			p, close := scimTestPeers(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/i/users" || r.URL.Query().Get("includePendingInvitations") != "true" {
					t.Error("unexpected account mutation or query", r.Method, r.URL)
					w.WriteHeader(500)
					return
				}
				if scenario == "absent" {
					_ = json.NewEncoder(w).Encode([]any{})
					return
				}
				account := map[string]any{"id": uid.String(), "team": team.String(), "managed_by": "scim", "status": "pending-invitation", "email_unvalidated": "synthetic@example.test", "sso_id": map[string]string{"scim_external_id": "synthetic@example.test"}}
				if scenario == "active" {
					account["status"] = "active"
				}
				if scenario == "foreign" {
					account["team"] = foreign.String()
				}
				if scenario == "wire" {
					account["managed_by"] = "wire"
				}
				if scenario == "no-identity" {
					delete(account, "sso_id")
				}
				_ = json.NewEncoder(w).Encode([]any{account})
			})
			defer close()
			if err := cleanupPendingSCIMUser(context.Background(), s, p, team, uid, ""); err != nil {
				t.Fatal(err)
			}
			var want []string
			if scenario == "pending" {
				want = []string{"external:synthetic@example.test", "meta"}
			}
			if scenario == "no-identity" {
				want = []string{"meta"}
			}
			if !reflect.DeepEqual(s.events, want) {
				t.Fatal("guard failed", s.events)
			}
		})
	}
}
