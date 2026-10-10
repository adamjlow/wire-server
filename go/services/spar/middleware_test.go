// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"bytes"
	"compress/gzip"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func compressedInput(t *testing.T, body string) []byte {
	t.Helper()
	var out bytes.Buffer
	z := gzip.NewWriter(&out)
	if _, err := z.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestGunzipSettingsAndUnusedBodies(t *testing.T) {
	store := &settingsSpy{}
	h := newHandler(Config{}, store)
	valid := compressedInput(t, `{"default_sso_code":null}`)
	for _, tc := range []struct {
		method, path string
		body         []byte
		code, writes int
	}{
		{"GET", "/i/status", []byte("invalid gzip"), 200, 0},
		{"GET", "/unknown", []byte("invalid gzip"), 404, 0},
		{"PUT", "/v0/i/sso/settings", valid, 404, 0},
		{"PUT", "/i/sso/settings", valid, 200, 1},
		{"PUT", "/i/sso/settings", []byte("invalid gzip"), 400, 1},
		{"PUT", "/i/sso/settings", valid[:len(valid)-5], 200, 2},
	} {
		req := httptest.NewRequest(tc.method, tc.path, bytes.NewReader(tc.body))
		req.Header.Set("Content-Encoding", "gzip")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if tc.method == "PUT" && tc.path == "/i/sso/settings" && string(tc.body) == "invalid gzip" {
			if w.Header().Get("Cache-Control") != "" || w.Header().Get("Pragma") != "" || !strings.Contains(w.Body.String(), `"label":"client-error"`) {
				t.Fatal("gzip middleware error boundary", w.Header(), w.Body.String())
			}
		}
		if w.Code != tc.code || store.writes != tc.writes {
			t.Fatal(tc.path, w.Code, store.writes)
		}
	}
}

func TestMetricsNormalizeCapturesAndCountConcurrentRequests(t *testing.T) {
	h := newHandler(Config{}, &settingsSpy{})
	var workers sync.WaitGroup
	for range 20 {
		workers.Go(func() { h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/sso/settings", nil)) })
	}
	workers.Wait()
	uid := "00000000-0000-4000-8000-000000000001"
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/i/scim/userinfo/"+uid, nil))
	req := httptest.NewRequest("GET", "/i/metrics", strings.NewReader("invalid gzip"))
	req.Header.Set("Content-Encoding", "gzip")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	body := w.Body.String()
	if w.Code != 200 || w.Header().Get("Content-Type") != "text/plain; version=0.0.4" || w.Header().Get("Cache-Control") != "" || w.Header().Get("Pragma") != "" {
		t.Fatal("metrics middleware ordering", w.Header())
	}
	if !strings.Contains(body, `http_request_duration_seconds_count{handler="/sso/settings",method="GET",status_code="200"} 20`) || !strings.Contains(body, `handler="/i/scim/userinfo/:user"`) || strings.Contains(body, uid) {
		t.Fatal("metrics count or capture labels", body)
	}
}

func TestMetricsOnlyInterceptGET(t *testing.T) {
	h := NewHandler(Config{})
	for _, method := range []string{"HEAD", "POST"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(method, "/i/metrics", nil))
		if w.Code != 404 || w.Header().Get("Cache-Control") != "no-cache, no-store" || w.Header().Get("Content-Type") != "" {
			t.Fatal("unsupported metrics method", method, w.Code, w.Header())
		}
	}
}
