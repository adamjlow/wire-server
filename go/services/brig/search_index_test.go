// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func TestSearchICUTransliterationAndConcurrentIsolation(t *testing.T) {
	for input, expected := range map[string]string{"": "", "JÖRG Straße": "jorg strasse", "Москва": "moskva", "北京": "bei jing", "CAFÉ\x00FIN": "cafe\x00fin"} {
		got, err := normalizedSearchName(input)
		if err != nil || got != expected {
			t.Fatalf("ICU normalization %q: %q, %v", input, got, err)
		}
	}
	if _, err := normalizedSearchName(string([]byte{0xff})); err == nil {
		t.Fatal("invalid UTF-8 accepted")
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 8 {
				got, err := normalizedSearchName("Αθήνα")
				if err != nil || got != "athena" {
					t.Errorf("concurrent ICU result %q, %v", got, err)
				}
			}
		})
	}
	wg.Wait()
}

func TestIndexDocumentSuspendedVisibilityAndTombstoneNulls(t *testing.T) {
	typ, status, managed := int32(2), int32(1), int32(1)
	r := indexUser{user: storedUser{ID: gocql.UUID{}, Name: "JÖRG", Type: &typ, Status: &status, Activated: true, ManagedBy: &managed}, created: time.Unix(1700000000, 123456789)}
	role := &indexRole{name: "partner", written: time.Unix(1700000001, 0)}
	doc, err := r.document("searchable-by-all-teams", role)
	if err != nil || doc.Name == nil || *doc.Name != "JÖRG" || doc.Normalized == nil || *doc.Normalized != "jorg" || doc.Status == nil || *doc.Status != "suspended" || doc.Type == nil || *doc.Type != "app" || doc.Created == nil || *doc.Created != "2023-11-14T22:13:20.123Z" || doc.Role == nil || *doc.Role != "partner" {
		t.Fatal("suspended account lost native search document", err)
	}
	for _, state := range []int32{2, 3, 4} {
		r.user.Status = &state
		doc, err := r.document("searchable-by-own-team", role)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(doc)
		var fields map[string]any
		_ = json.Unmarshal(data, &fields)
		if len(fields) != 18 || fields["id"] != r.user.ID.String() {
			t.Fatal("tombstone omitted native fields")
		}
		for name, value := range fields {
			if name != "id" && value != nil {
				t.Fatal("tombstone retained private indexed field", name)
			}
		}
	}
	r.user.Status = &status
	r.user.SSO = json.RawMessage(`{"tenant":"<Issuer xmlns=\"urn:oasis:names:tc:SAML:2.0:assertion\">https://synthetic.example/idp</Issuer>","subject":"<NameID xmlns=\"urn:oasis:names:tc:SAML:2.0:assertion\">Synthetic name</NameID>"}`)
	doc, err = r.document("searchable-by-own-team", nil)
	if err != nil || doc.SSO == nil || doc.SSO.NameID != "Synthetic name" || doc.SAMLIDP == nil || *doc.SAMLIDP != "https://synthetic.example/idp" || doc.SCIMExternal == nil || *doc.SCIMExternal != "Synthetic name" {
		t.Fatal("SAML index projection", err)
	}
	r.user.SSO = json.RawMessage(`{"scim_external_id":"synthetic-external"}`)
	doc, err = r.document("searchable-by-own-team", nil)
	if err != nil || doc.SSO != nil || doc.SCIMExternal == nil || *doc.SCIMExternal != "synthetic-external" {
		t.Fatal("SCIM external index projection", err)
	}
}

func TestIndexVersionNativeBloodhoundBounds(t *testing.T) {
	for _, version := range []int64{-1, 0, 1, 9200000000000000000, 9200000000000000001, math.MaxInt64} {
		want := version
		if want <= 0 || want > 9200000000000000000 {
			want = 1
		}
		if got := indexDocumentVersion(time.UnixMicro(version), nil); got != want {
			t.Fatal("native DocVersion bound", version, got, want)
		}
	}
	if got := indexDocumentVersion(time.UnixMicro(10), &indexRole{written: time.UnixMicro(20)}); got != 20 {
		t.Fatal("role timestamp not included")
	}
}

func TestSearchUpsertVersionConflictAndAdditionalWriteOrdering(t *testing.T) {
	var mutex sync.Mutex
	var paths []string
	status := http.StatusConflict
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mutex.Lock()
		defer mutex.Unlock()
		paths = append(paths, r.URL.Path)
		if r.Method != http.MethodPut || r.URL.Query().Get("version") != "42" || r.URL.Query().Get("version_type") != "external_gt" || r.Header.Get("Request-Id") != "synthetic-index" {
			t.Error("native upsert method/version/request ID")
		}
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil || len(body) != 18 || body["name"] != nil {
			t.Error("native tombstone object")
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte("synthetic private diagnostic"))
	}))
	defer server.Close()
	additional := "additional"
	writer, err := newSearchIndex(searchIndexConfig{URL: server.URL, Index: "main", AdditionalIndex: &additional})
	if err != nil {
		t.Fatal(err)
	}
	defer writer.close()
	version := int64(42)
	err = writer.upsert(context.Background(), indexDocument{ID: "synthetic-user"}, &version, "synthetic-index")
	mutex.Lock()
	if err != nil || strings.Join(paths, ",") != "/main/user/synthetic-user,/additional/user/synthetic-user" {
		t.Error("409 must continue additional write", err, paths)
	}
	status, paths = 500, nil
	mutex.Unlock()
	err = writer.upsert(context.Background(), indexDocument{ID: "synthetic-user"}, &version, "synthetic-index")
	mutex.Lock()
	defer mutex.Unlock()
	if err == nil || len(paths) != 1 || strings.Contains(err.Error(), "private") {
		t.Fatal("failed primary write continued or leaked", err, paths)
	}
}

type memoryIndexUserStore struct {
	user *indexUser
	err  error
}

func (s memoryIndexUserStore) indexUser(context.Context, gocql.UUID) (*indexUser, error) {
	return s.user, s.err
}

func TestSearchSyncMissingUserUnversionedAndStoreFailure(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.RawQuery != "" {
			t.Error("missing-user tombstone must have no version control")
		}
		w.WriteHeader(201)
	}))
	defer server.Close()
	writer, err := newSearchIndex(searchIndexConfig{URL: server.URL, Index: "main"})
	if err != nil {
		t.Fatal(err)
	}
	defer writer.close()
	service := searchIndexService{users: memoryIndexUserStore{}, writer: writer, team: func(context.Context, string, string, string) (string, *indexRole, error) {
		t.Error("missing user must not fetch team")
		return "", nil, nil
	}}
	if err := service.sync(context.Background(), gocql.UUID{}, "synthetic"); err != nil || calls.Load() != 1 {
		t.Fatal(err, calls.Load())
	}
	service.users = memoryIndexUserStore{err: errors.New("synthetic storage failure")}
	if err := service.sync(context.Background(), gocql.UUID{}, "synthetic"); err == nil || calls.Load() != 1 {
		t.Fatal("storage failure indexed empty fallback", err)
	}
}

func TestIndexTeamFeatureAndPermissionRole(t *testing.T) {
	for _, self := range []uint64{8191, 5951, 1587, 1025, 3, 8191 | (1 << 30)} {
		t.Run(strconv.FormatUint(self, 10), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.Header.Get("Request-Id") != "synthetic" {
					t.Error("native Galley method/request ID")
				}
				if strings.HasSuffix(r.URL.Path, "/features/searchVisibilityInbound") {
					_, _ = w.Write([]byte(`{"status":"enabled"}`))
					return
				}
				body, _ := io.ReadAll(r.Body)
				if string(body) != `{"ids":["00000000-0000-0000-0000-000000000000"]}` || r.Header.Get("Content-Type") != "application/json" {
					t.Error("native GET member selection body", string(body))
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"members": []any{map[string]any{"userId": gocql.UUID{}.String(), "permissions": map[string]any{"self": self, "copy": uint64(1)}, "permissionsWriteTime": "2023-11-14T22:13:20.123Z"}}})
			}))
			defer server.Close()
			host, port, _ := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
			n, _ := strconv.Atoi(port)
			peers := brigPeers{client: server.Client(), galley: Endpoint{Host: host, Port: n}}
			visibility, role, err := peers.indexTeam(context.Background(), "synthetic-team", gocql.UUID{}.String(), "synthetic")
			want := map[uint64]string{8191: "owner", 5951: "admin", 1587: "member", 1025: "partner"}[self&8191]
			if err != nil || visibility != "searchable-by-all-teams" || want == "" && role != nil || want != "" && (role == nil || role.name != want || role.written.UnixMilli() != 1700000000123) {
				t.Fatal("native role/visibility projection", err, role)
			}
		})
	}
}
