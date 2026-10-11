// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func TestProviderProfileUpdateAdmission(t *testing.T) {
	for _, body := range []string{`{}`, `{"name":null,"url":null,"description":null}`, `{"name":"Synthetic","url":"https://example.com/a+b","description":""}`, `{"name":"` + strings.Repeat("界", 128) + `"}`} {
		if _, err := decodeProviderProfileUpdate(json.RawMessage(body)); err != nil {
			t.Fatal("native optional/range update rejected", err)
		}
	}
	for _, body := range []string{`null`, `[]`, `{"name":""}`, `{"name":7}`, `{"name":"` + strings.Repeat("界", 129) + `"}`, `{"url":"http://example.com"}`, `{"url":"/relative"}`, `{"description":false}`} {
		if _, err := decodeProviderProfileUpdate(json.RawMessage(body)); err == nil {
			t.Fatal("invalid provider update accepted", body)
		}
	}
}

func TestProviderAccountGuardAndWriteOrdering(t *testing.T) {
	id, _ := gocql.ParseUUID("11111111-2222-3333-4444-555555555555")
	for _, tc := range []struct {
		name                  string
		enabled, missing      bool
		peerErr, storeErr     error
		want, lookups, writes int
	}{
		{name: "write", want: 200, lookups: 1, writes: 1},
		{name: "missing", missing: true, want: 404, lookups: 1},
		{name: "second-factor", enabled: true, want: 403},
		{name: "peer-failure", peerErr: errors.New("synthetic peer failure"), want: 500},
		{name: "storage-failure", storeErr: errors.New("synthetic storage failure"), want: 500, lookups: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lookups, writes := 0, 0
			h := providerAccountHTTP{
				secondFactorEnabled: func(context.Context, string) (bool, error) { return tc.enabled, tc.peerErr },
				lookup: func(_ context.Context, got gocql.UUID) (*providerAccount, error) {
					lookups++
					if got != id {
						t.Fatal("lookup escaped provider header scope")
					}
					if tc.missing || tc.storeErr != nil {
						return nil, tc.storeErr
					}
					return &providerAccount{ID: id, Name: "Synthetic", Email: "synthetic@example.com", URL: "https://example.com", Description: "Synthetic"}, nil
				},
				update: func(_ context.Context, got gocql.UUID, update providerProfileUpdate) error {
					writes++
					if got != id || update.name == nil || *update.name != "Updated" {
						t.Fatal("wrong provider write")
					}
					return nil
				},
			}
			r := httptest.NewRequest(http.MethodPut, "/provider", strings.NewReader(`{"name":"Updated"}`))
			r.Header.Set("Z-Provider", id.String())
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			h.handle(w, r, r.URL.Path)
			if w.Code != tc.want || lookups != tc.lookups || writes != tc.writes {
				t.Fatal("provider admission/write order", w.Code, lookups, writes)
			}
		})
	}
}

func TestProviderPublicProjectionAndHeaderBoundary(t *testing.T) {
	id, _ := gocql.ParseUUID("11111111-2222-3333-4444-555555555555")
	account := &providerAccount{ID: id, Name: "Synthetic", Email: "synthetic@example.com", URL: "https://example.com", Description: "Synthetic"}
	h := providerAccountHTTP{lookup: func(context.Context, gocql.UUID) (*providerAccount, error) { return account, nil }, secondFactorEnabled: func(context.Context, string) (bool, error) { return false, nil }}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		r := httptest.NewRequest(method, "/providers/"+id.String(), nil)
		r.Header.Set("Z-User", id.String())
		w := httptest.NewRecorder()
		if !h.handle(w, r, r.URL.Path) || w.Code != 200 {
			t.Fatal("public provider unavailable")
		}
		if method == http.MethodHead && w.Body.Len() != 0 {
			t.Fatal("HEAD emitted body")
		}
		if method == http.MethodGet {
			var got map[string]any
			if json.Unmarshal(w.Body.Bytes(), &got) != nil || len(got) != 5 || got["email"] != account.Email {
				t.Fatal("native public provider projection differs", got)
			}
		}
	}
	h.lookup = func(context.Context, gocql.UUID) (*providerAccount, error) {
		t.Fatal("storage accessed before header validation")
		return nil, nil
	}
	for _, header := range []string{"", "invalid"} {
		r := httptest.NewRequest(http.MethodGet, "/provider", nil)
		if header != "" {
			r.Header.Set("Z-Provider", header)
		}
		w := httptest.NewRecorder()
		h.handle(w, r, r.URL.Path)
		if w.Code != 400 {
			t.Fatal("invalid provider header accepted")
		}
	}
}

func TestProviderSecondFactorPeerFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		body          string
		enabled, fail bool
	}{
		{`{"sndFactorPasswordChallenge":{"status":"disabled"}}`, false, false},
		{`{"sndFactorPasswordChallenge":{"status":"enabled"}}`, true, false},
		{`{}`, false, true}, {`{"sndFactorPasswordChallenge":{"status":"unknown"}}`, false, true}, {`invalid`, false, true},
	} {
		peer := brigPeers{galley: Endpoint{Host: "synthetic.invalid", Port: 8085}, client: &http.Client{Transport: deletionRoundTripper(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path != "/i/feature-configs" || r.URL.RawQuery != "" || r.Header.Get("Request-Id") != "synthetic-provider" || r.Header.Get("Z-User") != "" {
				t.Fatal("provider guard request differs")
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(tc.body)), Header: http.Header{}}, nil
		})}}
		enabled, err := peer.providerSecondFactorEnabled(context.Background(), "synthetic-provider")
		if enabled != tc.enabled || (err != nil) != tc.fail {
			t.Fatal("provider feature guard", enabled, err)
		}
	}
}
