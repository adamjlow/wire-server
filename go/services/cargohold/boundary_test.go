package cargohold

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestFederationReferenceAuthorizationAndTokenPresence(t *testing.T) {
	owner := "11111111-1111-4111-8111-111111111111"
	other := "22222222-2222-4222-8222-222222222222"
	// Malformed legacy audit metadata is omitted by the typed reference copy.
	store := &fixtureStore{meta: map[string]string{"user": owner, "token": "private"}, stored: []byte("fixture")}
	s := NewServer(Config{}, store)
	for _, test := range []struct {
		user      string
		token     *string
		available bool
	}{{owner, nil, true}, {owner, pointer("wrong"), true}, {other, nil, false}, {other, pointer("wrong"), false}, {other, pointer("private"), true}} {
		for _, route := range []string{"get-asset", "stream-asset"} {
			b, _ := json.Marshal(assetRequest{User: test.user, Key: "3-2-33333333-3333-4333-8333-333333333333", Token: test.token})
			r := httptest.NewRequest("POST", "/federation/"+route, strings.NewReader(string(b)))
			r.Header.Set("Wire-Origin-Domain", "foreign.example.com")
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			if route == "get-asset" {
				var value map[string]bool
				if json.Unmarshal(w.Body.Bytes(), &value) != nil || value["available"] != test.available {
					t.Fatalf("availability %s", w.Body.String())
				}
			} else {
				if test.available && (w.Code != 200 || w.Body.String() != "fixture") {
					t.Fatalf("stream status/body %d %s", w.Code, w.Body.String())
				}
				if !test.available && w.Code != 404 {
					t.Fatalf("private stream leaked: %d", w.Code)
				}
			}
		}
	}
	if tokenMatches(map[string]string{}, pointer("")) || tokenMatches(map[string]string{"token": ""}, nil) {
		t.Fatal("absent and present-empty token conflated")
	}
}

func pointer(s string) *string { return &s }

func TestBrigLimitSelectionAndErrorBoundary(t *testing.T) {
	var status string
	var team any
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/i/users/accounts-by" || r.Method != "POST" {
			t.Errorf("wrong Brig route: %s %s", r.Method, r.URL.Path)
		}
		var b map[string]any
		if json.NewDecoder(r.Body).Decode(&b) != nil || b["include_users_without_identity"] != true || b["include_pending_invitations"] != false {
			t.Error("wrong accounts query")
		}
		writeJSON(w, 200, []map[string]any{{"status": status, "team": team}})
	}))
	defer peer.Close()
	host, port, _ := net.SplitHostPort(strings.TrimPrefix(peer.URL, "http://"))
	n, _ := strconv.Atoi(port)
	var c Config
	c.Brig = Endpoint{Host: host, Port: n}
	c.Settings.MaxTotalBytes = 20
	c.Settings.MaxTotalBytesStrict = 10
	s := NewServer(c, &fixtureStore{})
	for _, test := range []struct {
		status string
		team   any
		limit  int64
		denied bool
	}{{"active", nil, 10, false}, {"active", "team-id", 20, false}, {"ephemeral", nil, 10, false}, {"suspended", nil, 0, true}, {"deleted", nil, 0, true}} {
		status, team = test.status, test.team
		limit, err := s.userLimit(httptest.NewRequest("POST", "/assets", nil), "11111111-1111-4111-8111-111111111111")
		if limit != test.limit || (err != nil) != test.denied {
			t.Fatalf("%+v: %d %v", test, limit, err)
		}
	}
}

func TestVersionAndTokenMutationBoundaries(t *testing.T) {
	owner := "11111111-1111-4111-8111-111111111111"
	var c Config
	c.Settings.FederationDomain = "example.com"
	c.Settings.DisabledAPIVersions = []any{"v1"}
	store := &fixtureStore{meta: map[string]string{"user": owner, "token": "old", "wire-metadata": "audit"}}
	s := NewServer(c, store)
	for _, path := range []string{"/v1/assets/v3/key", "/v20/assets/key", "/v2/i/status"} {
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("Z-User", owner)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 404 || !strings.Contains(w.Body.String(), "unsupported-version") {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
	key := "3-2-33333333-3333-4333-8333-333333333333"
	for _, method := range []string{"POST", "DELETE"} {
		r := httptest.NewRequest(method, "/v2/assets/"+key+"/token", nil)
		r.Header.Set("Z-User", owner)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 200 || store.meta["wire-metadata"] != "" {
			t.Fatalf("%s token copy: %d %s", method, w.Code, w.Body.String())
		}
		if method == "POST" && (store.meta["token"] == "" || store.meta["token"] == "old") {
			t.Fatal("token not renewed")
		}
	}
	if _, present := store.meta["token"]; present {
		t.Fatal("deleted token still present")
	}
	// Both principal type and owner ID matter on public routes.
	store.meta = map[string]string{"bot": owner, "token": "private"}
	r := httptest.NewRequest("GET", fmt.Sprintf("/v2/assets/example.com/%s", key), nil)
	r.Header.Set("Z-User", owner)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 404 {
		t.Fatal("cross-principal private read permitted")
	}
}
