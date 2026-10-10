// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type scimTestLookup struct {
	info  *tokenInfo
	calls int
}

func (s *scimTestLookup) lookupToken(_ context.Context, token string) (*tokenInfo, error) {
	s.calls++
	if token != "synthetic-token" {
		return nil, nil
	}
	return s.info, nil
}
func scimTestPeers(t *testing.T, handler http.HandlerFunc) (peerClient, func()) {
	t.Helper()
	server := httptest.NewServer(handler)
	host, port, _ := net.SplitHostPort(server.Listener.Addr().String())
	n, _ := strconv.Atoi(port)
	return peerClient{config: Config{Brig: Endpoint{Host: host, Port: n}}, http: server.Client()}, server.Close
}
func scimTestID(t *testing.T, suffix string) gocql.UUID {
	t.Helper()
	id, err := gocql.ParseUUID("00000000-0000-4000-8000-" + suffix)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestSCIMUnauthenticatedRequestsNeverReachBrig(t *testing.T) {
	for _, header := range []string{"", "Bearer missing", "bearer synthetic-token", "Basic synthetic"} {
		t.Run(header, func(t *testing.T) {
			lookup := &scimTestLookup{}
			p, close := scimTestPeers(t, func(http.ResponseWriter, *http.Request) { t.Error("unauthorized request reached Brig") })
			defer close()
			r := httptest.NewRequest("GET", "/scim/v2/Groups", nil)
			if header != "" {
				r.Header.Set("Authorization", header)
			}
			w := httptest.NewRecorder()
			if !serveSCIMGroups(w, r, Config{}, lookup, p) || w.Code != 401 && w.Code != 400 {
				t.Fatal("invalid token granted access", w.Code)
			}
			var body map[string]any
			if json.Unmarshal(w.Body.Bytes(), &body) != nil || body["status"] != strconv.Itoa(w.Code) || w.Header().Get("Content-Type") != "application/scim+json;charset=utf-8" {
				t.Fatal("invalid SCIM error contract")
			}
			if strings.Contains(w.Body.String(), header) && header != "" {
				t.Fatal("authorization credential echoed")
			}
		})
	}
}
func TestSCIMGroupListScopesAndSerializes(t *testing.T) {
	team, id, member := scimTestID(t, "000000000001"), scimTestID(t, "000000000002"), scimTestID(t, "000000000003")
	p, close := scimTestPeers(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/i/user-groups/"+team.String() || r.URL.Query().Get("managedBy") != "scim" || r.URL.Query().Get("startIndex") != "2" || r.URL.Query().Get("count") != "0" || r.URL.Query().Get("nameContains") != "synthetic" {
			t.Error("wrong team/filter/pagination RPC", r.URL)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"page": []brigGroup{{ID: id, Name: "synthetic", Members: []gocql.UUID{member}, ManagedBy: "scim", CreatedAt: time.Date(2026, 10, 10, 12, 0, 0, 123000000, time.UTC)}}, "total": 8})
	})
	defer close()
	lookup := &scimTestLookup{info: &tokenInfo{Team: team}}
	r := httptest.NewRequest("GET", `/scim/v2/Groups?filter=displayName%20CO%20%22synthetic%22&startIndex=3&count=-1`, nil)
	r.Header.Set("Authorization", "Bearer synthetic-token")
	r.Header.Set("Z-Team", member.String())
	w := httptest.NewRecorder()
	if !serveSCIMGroups(w, r, Config{SCIMBaseURI: "https://synthetic.example/scim/v2"}, lookup, p) || w.Code != 200 {
		t.Fatal("listing failed", w.Code, w.Body.String())
	}
	var body struct {
		StartIndex int `json:"startIndex"`
		Total      int `json:"totalResults"`
		Resources  []struct {
			Members []map[string]string `json:"members"`
			Meta    map[string]any      `json:"meta"`
		} `json:"Resources"`
	}
	if json.Unmarshal(w.Body.Bytes(), &body) != nil || body.StartIndex != 3 || body.Total != 8 || len(body.Resources) != 1 {
		t.Fatal("SCIM page contract")
	}
	g := body.Resources[0]
	if g.Meta["version"] != `W/"v1"` || g.Meta["created"] != "2026-10-10T12:00:00.123Z" || g.Meta["lastModified"] != g.Meta["created"] || g.Members[0]["$ref"] != "https://synthetic.example/scim/v2/Users/"+member.String() {
		t.Fatal("SCIM group metadata contract")
	}
}
func TestSCIMGroupUpdateRejectsInvalidAddedMembers(t *testing.T) {
	for _, kind := range []string{"missing", "wrong-team", "wire-managed"} {
		t.Run(kind, func(t *testing.T) {
			team, id, member := scimTestID(t, "000000000001"), scimTestID(t, "000000000002"), scimTestID(t, "000000000003")
			requests := 0
			p, close := scimTestPeers(t, func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.Method != "GET" {
					t.Error("invalid member caused group mutation")
				}
				if strings.HasPrefix(r.URL.Path, "/i/user-groups/") {
					_ = json.NewEncoder(w).Encode(brigGroup{ID: id, Name: "before", Members: []gocql.UUID{}, ManagedBy: "scim", CreatedAt: time.Now()})
					return
				}
				if r.URL.Path != "/i/users" || r.URL.Query().Get("ids") != member.String() {
					t.Error("incorrect added-member lookup")
				}
				accounts := []brigAccount{}
				account := brigAccount{ID: member, Team: &team, ManagedBy: "scim"}
				switch kind {
				case "wrong-team":
					account.Team = &member
				case "wire-managed":
					account.ManagedBy = "wire"
				}
				if kind != "missing" {
					accounts = append(accounts, account)
				}
				_ = json.NewEncoder(w).Encode(accounts)
			})
			defer close()
			_, err := p.updateSCIMGroup(context.Background(), team, id, scimGroupInput{Name: "after", Members: []gocql.UUID{member}}, "synthetic-request")
			var failure *scimError
			if !errors.As(err, &failure) || requests != 2 {
				t.Fatal("invalid member accepted", err, requests)
			}
			if kind == "missing" && failure.Status != 404 || kind != "missing" && failure.Status != 400 {
				t.Fatal("member error semantics", failure)
			}
		})
	}
}
func TestSCIMGroupCaseFoldAndNativeBounds(t *testing.T) {
	r := httptest.NewRequest("POST", "/scim/v2/Groups", strings.NewReader(`{"SCHEMAS":["synthetic-schema"],"DISPLAYNAME":"Synthetic Name","MEMBERS":[{"VALUE":"00000000-0000-4000-8000-000000000001","TYPE":"User","$REF":"ignored"}]}`))
	r.Header.Set("Content-Type", "application/scim+json")
	object, err := scimJSON(r)
	if err != nil {
		t.Fatal(err)
	}
	group, err := parseSCIMGroup(object)
	if err != nil || group.Name != "Synthetic Name" || len(group.rawMembers) != 1 {
		t.Fatal("native field folding altered values", err)
	}
	if scimGroupName(strings.Repeat("✈", 4000)) != nil || scimGroupName(strings.Repeat("✈", 4001)) == nil || scimGroupName("") == nil {
		t.Fatal("group name rune bounds differ")
	}
}

func TestSCIMGroupCreationChecksMembersBeforeMutation(t *testing.T) {
	for _, managed := range []string{"scim", "wire"} {
		t.Run(managed, func(t *testing.T) {
			team, id, member := scimTestID(t, "000000000001"), scimTestID(t, "000000000002"), scimTestID(t, "000000000003")
			requests := []string{}
			p, close := scimTestPeers(t, func(w http.ResponseWriter, r *http.Request) {
				requests = append(requests, r.Method+" "+r.URL.Path)
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				if r.URL.Path == "/i/users/accounts-by" {
					ids, ok := body["ids"].([]any)
					if !ok || len(ids) != 1 || ids[0] != member.String() || body["include_pending_invitations"] != false {
						t.Error("incorrect native account lookup")
					}
					_ = json.NewEncoder(w).Encode([]brigAccount{{ID: member, Team: &team, ManagedBy: managed}})
					return
				}
				if managed != "scim" {
					t.Error("wire-managed member reached mutation")
				}
				if r.URL.Path != "/i/user-groups/full" || body["managed_by"] != "scim" || body["team_id"] != team.String() || body["creator_user_id"] != nil {
					t.Error("incorrect native creation envelope")
				}
				input, ok := body["new_group"].(map[string]any)
				if !ok || input["name"] != "synthetic group" {
					t.Error("incorrect group input")
				}
				w.WriteHeader(201)
				_ = json.NewEncoder(w).Encode(brigGroup{ID: id, Name: "synthetic group", Members: []gocql.UUID{member}, ManagedBy: "scim", CreatedAt: time.Now()})
			})
			defer close()
			group, err := p.createSCIMGroup(context.Background(), team, scimGroupInput{Name: "synthetic group", Members: []gocql.UUID{member}}, "synthetic-request")
			if managed == "scim" {
				if err != nil || group == nil || group.ID != id || len(requests) != 2 {
					t.Fatal("group creation failed", err, requests)
				}
			} else {
				var e *scimError
				if !errors.As(err, &e) || e.Status != 400 || group != nil || len(requests) != 1 {
					t.Fatal("managedBy guard failed", err, requests)
				}
			}
		})
	}
}
