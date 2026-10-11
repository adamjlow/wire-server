// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/apache/cassandra-gocql-driver/v2"
	"go.yaml.in/yaml/v3"
)

func TestPrekeyMapDecodeNormalizationAndLimits(t *testing.T) {
	user := gocql.TimeUUID().String()
	body := fmt.Sprintf(`{"Example.com":{"%s":["10","000A","f","a"],"%s":[]}}`, user, user)
	result, err := decodePrekeyClients(strings.NewReader(body), true)
	if err != nil || strings.Join(result["example.com"][user], ",") != "a,f,10" {
		t.Fatal("first duplicate/client numeric set/domain decode changed")
	}
	for _, body := range []string{`null`, `[]`, `{"example.com":[]}`, `{"example.com":{"invalid":[]}}`, fmt.Sprintf(`{"example.com":{"%s":null}}`, user), fmt.Sprintf(`{"example.com":{"%s":[null]}}`, user), fmt.Sprintf(`{"example.com":{"%s":[1]}}`, user), `{} {}`} {
		if _, err := decodePrekeyClients(strings.NewReader(body), true); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
	guardCalls := 0
	s := prekeyClaimService{domain: "example.com", maxUsers: 1, guard: func(context.Context, gocql.UUID, map[string][]string, string) error { guardCalls++; return nil }, claim: func(context.Context, gocql.UUID, string) (*uncheckedPrekey, error) {
		return &uncheckedPrekey{ID: 65535, Key: "synthetic"}, nil
	}}
	h := newHandler(Config{}, &brigReadHandlers{prekeys: &s})
	for _, tc := range []struct {
		path, body string
		status     int
		field      bool
	}{{"/v1/users/prekeys", fmt.Sprintf(`{"%s":["a"]}`, user), 200, false}, {"/v2/users/prekeys", "{}", 404, false}, {"/v3/users/list-prekeys", fmt.Sprintf(`{"example.com":{"%s":["a"]}}`, user), 200, false}, {"/v4/users/list-prekeys", fmt.Sprintf(`{"example.com":{"%s":["a"]}}`, user), 200, true}, {"/v19/users/list-prekeys", fmt.Sprintf(`{"example.com":{"%s":[]},"remote.example":{"%s":[]}}`, user, user), 403, false}} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", tc.path, strings.NewReader(tc.body))
		r.Header.Set("Z-User", user)
		r.Header.Set("Content-Type", "application/json")
		h.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("%s: %d %s", tc.path, w.Code, w.Body.String())
		}
		if tc.status == 200 && strings.Contains(w.Body.String(), `"qualified_user_client_prekeys"`) != tc.field {
			t.Fatal("versioned response wrapper changed")
		}
		if strings.Contains(w.Body.String(), `"failed_to_list"`) {
			t.Fatal("empty failed list must be omitted")
		}
	}
	if guardCalls != 3 {
		t.Fatal("limit/version failure performed guard/claims")
	}
	for _, text := range []string{"settings:\n  setMaxConvSize: 0", "settings:\n  setMaxConvSize: 65535"} {
		var c Config
		if yaml.Unmarshal([]byte(text), &c) != nil || c.Settings.MaxConversationSize == nil {
			t.Fatal("native Word16 configuration lost")
		}
	}
	var c Config
	if yaml.Unmarshal([]byte("settings:\n  setMaxConvSize: 65536"), &c) == nil {
		t.Fatal("native Word16 bound not enforced")
	}
}

func TestMultiPrekeyChunksOrderAndMissingNull(t *testing.T) {
	users := map[string][]string{}
	ordered := []string{}
	for n := 0; n < 17; n++ {
		id := fmt.Sprintf("00000000-0000-0000-0000-%012x", n+1)
		users[id] = []string{"10", "a"}
		ordered = append(ordered, id)
	}
	started := make(chan string, 32)
	release := make(chan struct{})
	var active, peak atomic.Int32
	traces := map[string][]string{}
	var lock sync.Mutex
	cleaned := 0
	guardCalls := 0
	s := prekeyClaimService{guard: func(_ context.Context, _ gocql.UUID, c map[string][]string, _ string) error {
		guardCalls++
		if len(c) != 17 {
			t.Fatal("guard must receive full user map")
		}
		return nil
	}, claim: func(_ context.Context, u gocql.UUID, id string) (*uncheckedPrekey, error) {
		count := active.Add(1)
		for {
			old := peak.Load()
			if count <= old || peak.CompareAndSwap(old, count) {
				break
			}
		}
		lock.Lock()
		traces[u.String()] = append(traces[u.String()], id)
		lock.Unlock()
		if id == "a" {
			started <- u.String()
			<-release
		}
		active.Add(-1)
		if id == "10" {
			return nil, nil
		}
		return &uncheckedPrekey{ID: 7, Key: "synthetic"}, nil
	}, lookup: func(context.Context, gocql.UUID, string) (*storedClient, error) { return &storedClient{ID: "10"}, nil }, remove: func(_ context.Context, _ gocql.UUID, conn *string, _ storedClient) error {
		if conn != nil {
			t.Error("noPrekeys origin must be Nothing")
		}
		lock.Lock()
		cleaned++
		lock.Unlock()
		return nil
	}}
	type answer struct {
		result userClientPrekeys
		err    error
	}
	done := make(chan answer, 1)
	go func() {
		r, err := s.multiLocal(context.Background(), gocql.UUID{}, users, "synthetic")
		done <- answer{r, err}
	}()
	for n := 0; n < 16; n++ {
		if id := <-started; id == ordered[16] {
			t.Error("second chunk started before first completed")
		}
	}
	close(release)
	result := <-done
	if result.err != nil || len(result.result) != 17 || guardCalls != 1 || peak.Load() != 16 || cleaned != 17 {
		t.Fatalf("result/chunk/guard/cleanup changed: peak=%d cleanup=%d", peak.Load(), cleaned)
	}
	for user, ids := range traces {
		if strings.Join(ids, ",") != "a,10" || result.result[user]["10"] != nil {
			t.Fatal("per-user numeric order/null result changed")
		}
	}
	body, _ := json.Marshal(result.result)
	if strings.Count(string(body), `"10":null`) != 17 {
		t.Fatal("missing keys omitted instead of null")
	}
}
