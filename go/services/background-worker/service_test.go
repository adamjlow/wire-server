// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSourceHealthAndReadinessOrdering(t *testing.T) {
	s := New(Config{}, GalleyConfig{})
	for _, tc := range []struct {
		method, path string
		code         int
		body         string
	}{{"GET", "/i/status", 200, ""}, {"POST", "/i/status", 405, ""}, {"GET", "/i/status/workers", 500, "These workers are not working: " + strings.Join(workerNames, ", ")}, {"GET", "/unknown", 404, "404 page not found\n"}} {
		r := httptest.NewRecorder()
		s.Handler().ServeHTTP(r, httptest.NewRequest(tc.method, tc.path, nil))
		if r.Code != tc.code || r.Body.String() != tc.body {
			t.Fatalf("%s: %d %q", tc.path, r.Code, r.Body.String())
		}
	}
	for i := range workerNames {
		s.mark(i, true)
	}
	r := httptest.NewRecorder()
	s.Handler().ServeHTTP(r, httptest.NewRequest("GET", "/i/status/workers", nil))
	if r.Code != 200 || r.Body.Len() != 0 {
		t.Fatal("healthy source response changed")
	}
	s.mark(1, false)
	r = httptest.NewRecorder()
	s.Handler().ServeHTTP(r, httptest.NewRequest("GET", "/i/status/workers", nil))
	if r.Code != 500 || r.Body.String() != "These workers are not working: dead-user-notification-watcher" {
		t.Fatal("worker failure ordering changed")
	}
}
