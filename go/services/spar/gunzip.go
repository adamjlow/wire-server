// SPDX-License-Identifier: AGPL-3.0-or-later
package spar

import (
	"compress/gzip"
	"errors"
	"io"
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
