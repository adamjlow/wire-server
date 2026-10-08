package gundeck

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGzipNegotiationAndStreamingResponse(t *testing.T) {
	for _, encoding := range []string{"gzip", "br, gzip;q=0.5"} {
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.Header.Set("Accept-Encoding", encoding)
		target := httptest.NewRecorder()
		writer := &gzipResponse{ResponseWriter: target, accepted: gzipAccepted(request)}
		writer.Header().Set("Content-Type", "application/json;charset=utf-8")
		_, _ = writer.Write([]byte(`{"ok":true}`))
		writer.Close()
		reader, err := gzip.NewReader(target.Body)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(reader)
		_ = reader.Close()
		if string(body) != `{"ok":true}` || target.Header().Get("Content-Encoding") != "gzip" || target.Header().Get("Vary") != "Accept-Encoding" {
			t.Fatal("gzip response mismatch")
		}
	}
	for _, encoding := range []string{"", "gzip;q=0", "gzip;q=bad", "br"} {
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.Header.Set("Accept-Encoding", encoding)
		if gzipAccepted(request) {
			t.Fatalf("accepted %q", encoding)
		}
	}
	for _, kind := range []string{"short", "partial", "encoded", "plain"} {
		target := httptest.NewRecorder()
		writer := &gzipResponse{ResponseWriter: target, accepted: true}
		writer.Header().Set("Content-Type", "application/json")
		status := 200
		switch kind {
		case "short":
			writer.Header().Set("Content-Length", "10")
		case "partial":
			status = 206
		case "encoded":
			writer.Header().Set("Content-Encoding", "br")
		case "plain":
			writer.Header().Del("Content-Type")
		}
		writer.WriteHeader(status)
		_, _ = writer.Write([]byte(strings.Repeat("a", 10)))
		writer.Close()
		if target.Header().Get("Content-Encoding") == "gzip" {
			t.Fatalf("compressed %s response", kind)
		}
	}
}
