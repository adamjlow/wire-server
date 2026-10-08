package gundeck

import (
	"compress/gzip"
	"net/http"
	"strconv"
	"strings"
)

// Match wai-extra default GzipSettings: only accepted gzip, compressible MIME,
// no existing encoding or partial content; explicit lengths below 860 skip.
// Streaming JSON without an explicit length remains eligible.
type gzipResponse struct {
	http.ResponseWriter
	accepted   bool
	written    bool
	compressor *gzip.Writer
}

func gzipAccepted(r *http.Request) bool {
	if strings.Contains(r.UserAgent(), "MSIE 6") {
		return false
	}
	for _, entry := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		parts := strings.Split(strings.TrimSpace(entry), ";")
		if parts[0] != "gzip" {
			continue
		}
		quality := 1.0
		for _, parameter := range parts[1:] {
			pair := strings.SplitN(strings.TrimSpace(parameter), "=", 2)
			if len(pair) != 2 || pair[0] != "q" {
				quality = 0
				break
			}
			value, err := strconv.ParseFloat(pair[1], 64)
			if err != nil || value < 0 || value > 1 {
				quality = 0
				break
			}
			quality = value
		}
		if quality > 0 {
			return true
		}
	}
	return false
}
func (w *gzipResponse) WriteHeader(status int) {
	if w.written {
		return
	}
	w.written = true
	header := w.Header()
	found := false
	for _, vary := range strings.Split(header.Get("Vary"), ",") {
		if strings.EqualFold(strings.TrimSpace(vary), "Accept-Encoding") {
			found = true
		}
	}
	if !found {
		if current := header.Get("Vary"); current != "" {
			header.Set("Vary", "Accept-Encoding, "+current)
		} else {
			header.Set("Vary", "Accept-Encoding")
		}
	}
	mime := strings.SplitN(header.Get("Content-Type"), ";", 2)[0]
	compressible := strings.HasPrefix(mime, "text/") || mime == "application/json" || mime == "application/javascript" || mime == "application/ecmascript" || mime == "image/x-icon"
	small := false
	if length := header.Get("Content-Length"); length != "" {
		if n, err := strconv.ParseInt(length, 10, 64); err == nil {
			small = n < 860
		}
	}
	if w.accepted && compressible && !small && status != 206 && header.Get("Content-Encoding") == "" {
		header.Set("Content-Encoding", "gzip")
		header.Del("Content-Length")
		w.compressor = gzip.NewWriter(w.ResponseWriter)
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *gzipResponse) Write(data []byte) (int, error) {
	if !w.written {
		w.WriteHeader(http.StatusOK)
	}
	if w.compressor != nil {
		return w.compressor.Write(data)
	}
	return w.ResponseWriter.Write(data)
}
func (w *gzipResponse) Close() {
	if w.compressor != nil {
		_ = w.compressor.Close()
	}
}
