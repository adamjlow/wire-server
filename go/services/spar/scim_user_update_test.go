// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type scimUpdateBoundary struct {
	scimUserBoundary
	existing gocql.UUID
	metadata scimUserMeta
}

func (s *scimUpdateBoundary) scimExternal(context.Context, gocql.UUID, string) (*gocql.UUID, error) {
	return &s.existing, nil
}
func (s *scimUpdateBoundary) scimMeta(context.Context, gocql.UUID) (*scimUserMeta, error) {
	m := s.metadata
	return &m, nil
}

func TestSCIMNoopUpdateRetainsMetadata(t *testing.T) {
	uid, team := scimTestID(t, "000000000001"), scimTestID(t, "000000000002")
	stamp := time.Date(2020, 1, 2, 3, 4, 5, 6000000, time.UTC)
	s := &scimUpdateBoundary{existing: uid, metadata: scimUserMeta{Created: stamp, Updated: stamp}}
	p, close := scimTestPeers(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Error("no-op update called mutating peer", r.Method, r.URL)
			w.WriteHeader(500)
			return
		}
		switch {
		case r.URL.Path == "/i/users":
			_ = json.NewEncoder(w).Encode([]any{map[string]any{"id": uid.String(), "team": team.String(), "name": "Synthetic", "handle": "synthetic", "email": "synthetic@example.test", "sso_id": map[string]string{"scim_external_id": "external"}, "locale": "en", "status": "active", "managed_by": "scim"}})
		case strings.HasSuffix(r.URL.Path, "/rich-info"):
			_ = json.NewEncoder(w).Encode(richInfo{}.object())
		case strings.Contains(r.URL.Path, "/members/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"permissions": map[string]int{"self": 1587, "copy": 1587}})
		case r.URL.Path == "/i/users/locale":
			_ = json.NewEncoder(w).Encode(map[string]string{"locale": "en"})
		default:
			t.Error("unexpected no-op read", r.URL)
			w.WriteHeader(500)
		}
	})
	defer close()
	p.config.Galley = p.config.Brig
	language, role := "en", "member"
	user := validSCIMUser{External: "external", Handle: "synthetic", Name: "Synthetic", Email: &scimEmail{Value: "synthetic@example.test"}, RichInfo: richInfo{}, Active: true, Language: &language, Role: &role}
	limit := 1000
	stored, err := updateSCIMUser(context.Background(), Config{SCIMBaseURI: "https://synthetic.example/scim/v2", RichInfoLimit: &limit}, s, p, tokenInfo{Team: team}, nil, uid, user, "")
	if err != nil {
		t.Fatal(err)
	}
	meta := stored["meta"].(map[string]any)
	if meta["created"] != "2020-01-02T03:04:05.006Z" || meta["lastModified"] != meta["created"] || s.meta != nil {
		t.Fatal("no-op rewrote user metadata", meta, s.meta)
	}
}
func TestSCIMFilteredLookupNeverAdoptsForeignTeam(t *testing.T) {
	uid, team, foreign := scimTestID(t, "000000000001"), scimTestID(t, "000000000002"), scimTestID(t, "000000000003")
	calls := 0
	p, close := scimTestPeers(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" || r.URL.Path != "/i/users" {
			t.Error("foreign lookup had non-account effect", r.Method, r.URL)
		}
		_ = json.NewEncoder(w).Encode([]brigAccount{{ID: uid, Team: &foreign}})
	})
	defer close()
	body, err := listSCIMUsers(context.Background(), Config{}, &scimUserBoundary{}, p, tokenInfo{Team: team}, nil, url.Values{"filter": {`userName eq "synthetic"`}}, "")
	if err != nil || body["totalResults"] != 0 || calls != 2 {
		t.Fatal("foreign account exposed", body, err, calls)
	}
	calls = 0
	_, err = listSCIMUsers(context.Background(), Config{}, &scimUserBoundary{}, p, tokenInfo{Team: team}, nil, url.Values{}, "")
	if err == nil || calls != 0 {
		t.Fatal("unfiltered enumeration reached Brig", err, calls)
	}
}
func TestSCIMStatusUpdatePreservesNativeSpecialStates(t *testing.T) {
	for _, status := range []string{"pending-invitation", "ephemeral", "deleted"} {
		for _, active := range []bool{true, false} {
			if scimStatusTransition(status, active) != status {
				t.Fatal("special account state changed", status, active)
			}
		}
	}
}
