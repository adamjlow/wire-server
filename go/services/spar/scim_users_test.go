// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type scimUserBoundary struct {
	scimUserStore
	events []string
	meta   *scimUserMeta
	uid    gocql.UUID
}

func (s *scimUserBoundary) scimCreation(context.Context, gocql.UUID, string) (*scimCreation, error) {
	s.events = append(s.events, "creation-lookup")
	return nil, nil
}
func (s *scimUserBoundary) scimExternal(context.Context, gocql.UUID, string) (*gocql.UUID, error) {
	s.events = append(s.events, "external-lookup")
	return nil, nil
}
func (s *scimUserBoundary) setSCIMCreation(_ context.Context, _ gocql.UUID, _ string, uid gocql.UUID, creating bool) error {
	s.uid = uid
	event := "created"
	if creating {
		event = "creating"
	}
	s.events = append(s.events, event)
	return nil
}
func (s *scimUserBoundary) scimMeta(context.Context, gocql.UUID) (*scimUserMeta, error) {
	s.events = append(s.events, "metadata-read")
	return nil, nil
}
func (s *scimUserBoundary) writeSCIMMeta(_ context.Context, _ gocql.UUID, meta scimUserMeta) error {
	s.meta = &meta
	s.events = append(s.events, "metadata-write")
	return nil
}
func (s *scimUserBoundary) insertSCIMExternal(context.Context, gocql.UUID, string, gocql.UUID) error {
	s.events = append(s.events, "external-insert")
	return nil
}

func TestSCIMCreateInvitationPersistsNativePendingState(t *testing.T) {
	team := scimTestID(t, "000000000001")
	s := &scimUserBoundary{}
	emailType := "work"
	primary := false
	user := validSCIMUser{External: "synthetic@example.test", Handle: "synthetic", Name: "Synthetic", Email: &scimEmail{"synthetic@example.test", &emailType, &primary}, Active: true, RichInfo: richInfo{{"Office", "Synthetic"}}}
	p, close := scimTestPeers(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Request-Id") != "synthetic-request" {
			t.Error("request ID missing")
		}
		switch {
		case r.Method == "HEAD":
			if r.URL.Path != "/i/users/handles/synthetic" {
				t.Error("wrong handle path")
			}
			s.events = append(s.events, "handle-check")
			w.WriteHeader(404)
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/invitations"):
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["team_id"] != team.String() || body["user_id"] != s.uid.String() || body["external_id"] != user.External || body["role"] != "member" {
				t.Error("invalid native invitation envelope", body)
			}
			s.events = append(s.events, "invitation")
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": s.uid.String()})
		case r.Method == "PUT" && strings.HasSuffix(r.URL.Path, "/handle"):
			s.events = append(s.events, "handle-set")
		case r.Method == "PUT" && strings.HasSuffix(r.URL.Path, "/rich-info"):
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if _, ok := body["rich_info"].(map[string]any); !ok {
				t.Error("rich info requires versioned assoc list")
			}
			s.events = append(s.events, "rich-set")
		case r.URL.Path == "/i/users":
			if r.URL.Query().Get("includePendingInvitations") != "true" || r.URL.Query().Get("ids") != s.uid.String() {
				t.Error("missing pending account lookup")
			}
			s.events = append(s.events, "account-read")
			_ = json.NewEncoder(w).Encode([]any{map[string]any{"id": s.uid.String(), "team": team.String(), "name": user.Name, "handle": user.Handle, "status": "pending-invitation", "managed_by": "scim", "locale": "en", "email_unvalidated": user.Email.Value}})
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/rich-info"):
			s.events = append(s.events, "rich-read")
			_ = json.NewEncoder(w).Encode(user.RichInfo.object())
		case strings.Contains(r.URL.Path, "/members/"):
			s.events = append(s.events, "role-read")
			_ = json.NewEncoder(w).Encode(map[string]any{"permissions": map[string]any{"self": 1587, "copy": 1587}})
		case strings.HasSuffix(r.URL.Path, "/features/validateSAMLemails"):
			s.events = append(s.events, "email-feature")
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "disabled"})
		case r.URL.Path == "/i/self/email":
			if r.Header.Get("Z-User") != s.uid.String() || r.URL.Query().Get("activation") != "auto_activate" {
				t.Error("wrong activation envelope")
			}
			s.events = append(s.events, "email-update")
			w.WriteHeader(204)
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/status"):
			s.events = append(s.events, "status-read")
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "pending-invitation"})
		default:
			t.Error("unexpected peer effect", r.Method, r.URL)
			w.WriteHeader(500)
		}
	})
	defer close()
	p.config.Galley = p.config.Brig
	stored, err := createSCIMUser(context.Background(), Config{SCIMBaseURI: "https://synthetic.example/scim/v2"}, s, p, tokenInfo{Team: team}, user, "synthetic-request")
	if err != nil {
		t.Fatal(err)
	}
	if stored["active"] != false {
		t.Fatal("pending invitation must stay inactive despite default active request")
	}
	if s.meta == nil || s.meta.EmailType == nil || *s.meta.EmailType != "work" || s.meta.EmailPrimary == nil || *s.meta.EmailPrimary {
		t.Fatal("nullable metadata lost")
	}
	expected := []string{"creation-lookup", "external-lookup", "handle-check", "creating", "invitation", "handle-set", "rich-set", "account-read", "rich-read", "metadata-read", "role-read", "metadata-write", "external-insert", "email-feature", "email-update", "status-read", "created"}
	if !reflect.DeepEqual(s.events, expected) {
		t.Fatal("native persistence/effect ordering changed", s.events)
	}
}

func TestSCIMCreatePeerFailureKeepsCreatingState(t *testing.T) {
	s := &scimUserBoundary{}
	team := scimTestID(t, "000000000001")
	p, close := scimTestPeers(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "HEAD" {
			w.WriteHeader(404)
		} else {
			w.WriteHeader(503)
		}
	})
	defer close()
	_, err := createSCIMUser(context.Background(), Config{}, s, p, tokenInfo{Team: team}, validSCIMUser{External: "synthetic@example.test", Handle: "synthetic", Email: &scimEmail{Value: "synthetic@example.test"}}, "synthetic-request")
	if err == nil || !reflect.DeepEqual(s.events, []string{"creation-lookup", "external-lookup", "creating"}) {
		t.Fatal("failed creation was marked complete", s.events, err)
	}
}

func TestSCIMUserForeignTeamReadAndDeleteHaveNoEffects(t *testing.T) {
	team, foreign, uid := scimTestID(t, "000000000001"), scimTestID(t, "000000000002"), scimTestID(t, "000000000003")
	p, close := scimTestPeers(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/i/users" {
			t.Error("foreign account mutated", r.Method, r.URL)
		}
		_ = json.NewEncoder(w).Encode([]brigAccount{{ID: uid, Team: &foreign}})
	})
	defer close()
	s := &scimUserBoundary{}
	stored, err := getSCIMUser(context.Background(), Config{}, s, p, tokenInfo{Team: team}, nil, uid, "")
	if err != nil || stored != nil {
		t.Fatal("foreign account exposed", stored, err)
	}
	if err = deleteSCIMUser(context.Background(), s, p, tokenInfo{Team: team}, uid, ""); err != nil || len(s.events) != 0 {
		t.Fatal("foreign account local state mutated", err, s.events)
	}
}

func TestSCIMUserBodyPreservesRichAttributeCase(t *testing.T) {
	r := httptest.NewRequest("POST", "/scim/v2/Users", strings.NewReader(`{"USERNAME":"synthetic","externalId":"synthetic@example.test","urn:ietf:params:scim:schemas:extension:wire:1.0:User":{"OfficeName":"Synthetic"}}`))
	r.Header.Set("Content-Type", "application/scim+json")
	object, err := scimRawJSON(r)
	if err != nil {
		t.Fatal(err)
	}
	user, err := parseSCIMUser(object)
	if err != nil || user.UserName != "synthetic" || !reflect.DeepEqual(user.RichInfo, richInfo{{"OfficeName", "Synthetic"}}) {
		t.Fatal("core folding corrupted extra schema keys", user, err)
	}
	_, err = scimRawJSON(httptest.NewRequest("POST", "/scim/v2/Users", strings.NewReader(`{}`)))
	var failure *scimError
	if !errors.As(err, &failure) || failure.Status != 415 {
		t.Fatal("unsupported media type accepted")
	}
}
