// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestEmailDiscoveryGatesAndHostMatching(t *testing.T) {
	uid, team, first, second := scimTestID(t, "000000000001"), scimTestID(t, "000000000002"), scimTestID(t, "000000000003"), scimTestID(t, "000000000004")
	host := "synthetic.example"
	for _, scenario := range []string{"disabled", "empty", "non-sso", "no-team", "multiple", "default", "host", "unknown-host"} {
		t.Run(scenario, func(t *testing.T) {
			s := &cleanupBoundary{idps: []idpConfig{{ID: first, Extra: idpExtra{Team: team}}, {ID: second, Extra: idpExtra{Team: team, Domain: &host}}}}
			calls := 0
			p, close := scimTestPeers(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "GET" {
					t.Error("discovery mutated peer")
					w.WriteHeader(500)
					return
				}
				if r.URL.Path == "/i/users" {
					if len(r.URL.Query()) != 1 || r.URL.Query().Get("email") != "synthetic@example.test" {
						t.Error("wrong lookup or pending-invitation exposure", r.URL)
					}
					account := map[string]any{"id": uid.String(), "sso_id": map[string]string{"scim_external_id": "external"}}
					users := []any{account}
					if scenario == "empty" {
						users = []any{}
					}
					if scenario == "non-sso" {
						delete(account, "sso_id")
					}
					if scenario == "multiple" {
						users = append(users, account)
					}
					_ = json.NewEncoder(w).Encode(users)
					return
				}
				if r.URL.Path != "/i/users/"+uid.String()+"/team" {
					t.Error("wrong team lookup", r.URL)
				}
				if scenario == "no-team" {
					w.WriteHeader(404)
					return
				}
				_ = json.NewEncoder(w).Encode(team)
			})
			defer close()
			enabled := scenario != "disabled"
			c := p.config
			c.Galley = c.Brig
			c.EnableEmailDiscovery = &enabled
			selectedHost := ""
			if scenario == "host" {
				selectedHost = host
			}
			if scenario == "unknown-host" {
				selectedHost = "unknown.example"
			}
			id, err := discoverEmail(context.Background(), c, s, p, "synthetic@example.test", selectedHost, "")
			if scenario == "multiple" {
				if e, ok := err.(*apiError); !ok || e.Code != 500 || e.Message != "Internal error: Multiple users found for email address" {
					t.Fatal("inconsistent user response", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "default" {
				if id == nil || *id != first {
					t.Fatal("default selection", id)
				}
			} else if scenario == "host" {
				if id == nil || *id != second {
					t.Fatal("host selection", id)
				}
			} else if id != nil {
				t.Fatal("unexpected identity disclosure", id)
			}
			if scenario == "disabled" && calls != 0 {
				t.Fatal("disabled feature contacted peers")
			}
		})
	}
}
