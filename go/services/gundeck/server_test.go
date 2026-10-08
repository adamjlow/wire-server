package gundeck

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVersionSelectionRejectsDisabledAndVersionedInternalAPIs(t *testing.T) {
	s := &Server{}
	s.Config.Settings.Disabled = []string{"v3", "development"}
	for _, path := range []string{"/v3/notifications", "/v19/time", "/v9/i/status", "/v20/time"} {
		response := httptest.NewRecorder()
		s.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != 404 {
			t.Fatalf("path %s returned %d", path, response.Code)
		}
	}
	request := httptest.NewRequest(http.MethodGet, "/time", nil)
	request.Header.Set("X-Wire-API-Version", "9")
	response := httptest.NewRecorder()
	s.ServeHTTP(response, request)
	if response.Code != 404 {
		t.Fatal("forged version header selected public API")
	}
}
func TestGzipAppliesToWholeAPISurface(t *testing.T) {
	var body bytes.Buffer
	compressed := gzip.NewWriter(&body)
	_, _ = compressed.Write([]byte("{"))
	_ = compressed.Close()
	request := httptest.NewRequest(http.MethodPost, "/i/presences", &body)
	request.Header.Set("Content-Encoding", "gzip")
	response := httptest.NewRecorder()
	(&Server{}).ServeHTTP(response, request)
	if response.Code != 400 || response.Body.String() != "Unexpected end-of-input, expecting record key literal or }" {
		t.Fatal("presence gzip was not decoded", response.Body.String())
	}
}
