package cargohold

import (
	"context"
	"fmt"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

func body(settings, payload, length string) string {
	return fmt.Sprintf("--first\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n%s\r\n--different\r\nContent-Type: text/plain\r\nContent-Length: %s\r\nContent-MD5: ignored\r\n\r\n%s", len(settings), settings, length, payload)
}

func TestMultipartCompatibilityAndHeaderRejection(t *testing.T) {
	r, settings, n, err := parseUpload(strings.NewReader(body(`{"public":false}`, "payload", "7suffix")))
	if err != nil || settings.Public || n != 7 {
		t.Fatalf("parse: %#v %d %v", settings, n, err)
	}
	b, _ := io.ReadAll(r)
	if string(b) != "payload" {
		t.Fatalf("payload=%q", b)
	}
	for _, bad := range []string{
		strings.Replace(body(`{}`, "data", "4"), "Content-Length: 4", "Content-Length: 4\r\nContent-Length: 4", 1),
		strings.Replace(body(`{}`, "data", "4"), "Content-Length: 4", "Unexpected-Header: yes\r\nContent-Length: 4", 1),
		body(`{} {}`, "data", "4"),
		body(`{}`, "data", "-4"),
	} {
		if _, _, _, err := parseUpload(strings.NewReader(bad)); err == nil {
			t.Fatal("accepted malformed metadata/header")
		}
	}
}

type fixtureStore struct {
	meta   map[string]string
	stored []byte
	calls  int
}

func (s *fixtureStore) Upload(_ context.Context, _ string, meta map[string]string, r io.Reader, n int64) (int64, error) {
	s.calls++
	s.meta = meta
	b, e := io.ReadAll(io.LimitReader(r, n))
	s.stored = b
	return int64(len(b)), e
}
func (s *fixtureStore) Metadata(context.Context, string) (map[string]string, error) {
	return s.meta, nil
}
func (s *fixtureStore) SignedURL(context.Context, string, string) (string, error) {
	return "http://owned-store/fixture", nil
}
func (s *fixtureStore) Delete(context.Context, string) error { s.calls++; return nil }

func TestUploadLimitAndIncompletePersistedEffect(t *testing.T) {
	var c Config
	c.Settings.MaxTotalBytes = 10
	c.Settings.FederationDomain = "example.com"
	store := &fixtureStore{}
	server := NewServer(c, store)
	for _, test := range []struct {
		length      string
		code, calls int
	}{{"11", 413, 0}, {"5", 400, 1}} {
		r := httptest.NewRequest("POST", "/bot/assets", strings.NewReader(body(`{}`, "abc", test.length)))
		r.Header.Set("Z-Type", "bot")
		r.Header.Set("Z-Bot", "11111111-1111-4111-8111-111111111111")
		r.Header.Set("Content-Type", "multipart/mixed")
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if w.Code != test.code || store.calls != test.calls {
			t.Fatalf("code=%d calls=%d body=%s", w.Code, store.calls, w.Body.String())
		}
	}
	if string(store.stored) != "abc" || store.meta["token"] == "" {
		t.Fatal("partial upload effects lost")
	}
}

func TestPrincipalAndPrivateTokenBoundary(t *testing.T) {
	store := &fixtureStore{meta: map[string]string{"bot": "11111111-1111-4111-8111-111111111111", "token": "private"}}
	server := NewServer(Config{}, store)
	for _, test := range []struct {
		method, typ, owner, token string
		code                      int
	}{
		{"GET", "provider", "11111111-1111-4111-8111-111111111111", "", 403},
		{"GET", "bot", "22222222-2222-4222-8222-222222222222", "", 404},
		{"GET", "bot", "22222222-2222-4222-8222-222222222222", "private", 302},
		{"DELETE", "bot", "22222222-2222-4222-8222-222222222222", "private", 403},
	} {
		r := httptest.NewRequest(test.method, "/bot/assets/3-2-33333333-3333-4333-8333-333333333333", nil)
		r.Header.Set("Z-Type", test.typ)
		r.Header.Set("Z-Bot", test.owner)
		r.Header.Set("Asset-Token", test.token)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if w.Code != test.code {
			t.Fatalf("%+v: %d", test, w.Code)
		}
	}
	if store.calls != 0 {
		t.Fatal("unauthorized deletion reached store")
	}
}
