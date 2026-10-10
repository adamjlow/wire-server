// SPDX-License-Identifier: AGPL-3.0-or-later
package spar

import (
	"compress/gzip"
	"errors"
	"io"
	"net/http"
)

var errGunzip = errors.New("invalid request body compression")

// WAI gunzip is lazy: health, metadata and unknown routes do not decode an
// unused body. Decode only when the selected handler reads its input.
type gunzipBody struct {
	source  io.ReadCloser
	decoded *gzip.Reader
	failed  bool
}

func (b *gunzipBody) Read(p []byte) (int, error) {
	if b.failed {
		return 0, errGunzip
	}
	if b.decoded == nil {
		var err error
		b.decoded, err = gzip.NewReader(b.source)
		if err != nil {
			b.failed = true
			return 0, errGunzip
		}
	}
	n, err := b.decoded.Read(p)
	// Native zlib gunzip accepts a completed payload without a trailer. Do not
	// add a gzip integrity policy while migrating; the decoded JSON/XML parser
	// must still consume and validate the complete request before effects.
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return n, io.EOF
	}
	if err != nil && err != io.EOF {
		b.failed = true
		return n, errGunzip
	}
	return n, err
}

func (b *gunzipBody) Close() error {
	if b.decoded != nil {
		_ = b.decoded.Close()
	}
	return b.source.Close()
}

// Decode errors occur at the handler's body read. Native catchErrors replaces
// the inner response (and cache headers) with its ordinary client-error JSON.
// Spar handlers parse before writing or performing authentication effects.
type gunzipWriter struct {
	http.ResponseWriter
	body     *gunzipBody
	rejected bool
}

func (w *gunzipWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *gunzipWriter) WriteHeader(code int) {
	if w.rejected {
		return
	}
	if w.body.failed {
		w.rejected = true
		for key := range w.Header() {
			w.Header().Del(key)
		}
		wireError(w.ResponseWriter, http.StatusBadRequest, "client-error", "Invalid request body compression")
		return
	}
	w.ResponseWriter.WriteHeader(code)
}
func (w *gunzipWriter) Write(body []byte) (int, error) {
	if w.body.failed {
		w.WriteHeader(http.StatusBadRequest)
		return len(body), nil
	}
	return w.ResponseWriter.Write(body)
}
