// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLivenessAndVersionSurface(t *testing.T) {
	c := Config{}
	c.Settings.Domain = "example.com"
	c.Settings.DisabledVersions = []string{"v1", "development"}
	h := NewHandler(c)
	for _, method := range []string{"GET", "HEAD"} {
		r := httptest.NewRequest(method, "/i/status", nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 || w.Body.Len() != 0 {
			t.Fatal("liveness does not match source NoContent")
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api-version", nil))
	var info struct {
		Supported, Development []int
		Domain                 string
		Federation             bool
	}
	if err := json.Unmarshal(w.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || info.Domain != "example.com" || info.Federation || len(info.Supported) != 18 || len(info.Development) != 0 {
		t.Fatal("incorrect native version projection")
	}
	for _, v := range info.Supported {
		if v == 1 || v == 19 {
			t.Fatal("disabled version advertised")
		}
	}
	for _, path := range []string{"/v1/api-version", "/v19/api-version", "/v20/api-version", "/v01/api-version", "/v/api-version", "/v1/i/status"} {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.Header.Set("X-Wire-API-Version", "0")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		var failure struct{ Label string }
		if json.Unmarshal(w.Body.Bytes(), &failure) != nil || w.Code != 404 || failure.Label != "unsupported-version" {
			t.Fatalf("version boundary %s: %d %s", path, w.Code, w.Body.String())
		}
	}
}
