// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"
)

// Pinned wai-extra3.1.18/servant-multipart0.12.1 defaults. These are not
// Go's ParseForm/ParseMultipartForm limits. Native exceptions cross the
// outer WAI boundary; exceeding a limit must not enter authentication.
const samlFormLimit = 65336

var (
	errSAMLFormLimit = errors.New("native form parameter limit")
	errSAMLFormWire  = errors.New("native multipart transport limit")
	errSAMLFormType  = errors.New("unsupported SAML form content type")
)

type samlFormSource struct {
	body io.Reader
	left []byte
}

func (s *samlFormSource) chunk() ([]byte, error) {
	if len(s.left) != 0 {
		data := s.left
		s.left = nil
		return data, nil
	}
	data := make([]byte, 64<<10)
	n, err := s.body.Read(data)
	if n != 0 {
		// As with WAI's body popper, EOF follows the final nonempty chunk.
		return data[:n], nil
	}
	return nil, err
}

func (s *samlFormSource) line() ([]byte, error) {
	var line []byte
	for {
		data, err := s.chunk()
		if err != nil {
			return nil, err
		}
		end := bytes.IndexByte(data, '\n')
		if end >= 0 {
			line = append(line, data[:end]...)
			s.left = data[end+1:]
			if len(line) > 8190 {
				return nil, errSAMLFormWire
			}
			return bytes.TrimSuffix(line, []byte{'\r'}), nil
		}
		if len(line)+len(data) > 8190 {
			return nil, errSAMLFormWire
		}
		line = append(line, data...)
	}
}

// Native wrapTillBound counts each input chunk before finding the delimiter,
// including read-ahead bytes. Preserve that guard and split/carry semantics;
// do not infer an atomic parameter limit from its transport-dependent count.
func (s *samlFormSource) part(boundary []byte, field bool) ([]byte, bool, error) {
	var value, carry []byte
	read := 0
	for {
		data, err := s.chunk()
		if err != nil {
			if errors.Is(err, io.EOF) {
				if field {
					value = append(value, carry...)
				}
				return value, false, nil
			}
			return nil, false, err
		}
		read += len(data)
		if field && read > samlFormLimit {
			return nil, false, errSAMLFormWire
		}
		joined := append(carry, data...)
		if index := bytes.Index(joined, boundary); index >= 0 {
			before := bytes.TrimSuffix(joined[:index], []byte{'\r', '\n'})
			// Source killCRLF also removes a bare LF before a delimiter.
			before = bytes.TrimSuffix(before, []byte{'\n'})
			if field {
				value = append(value, before...)
			}
			s.left = joined[index+len(boundary):]
			return value, true, nil
		}
		keep := 0
		for size := 1; size < len(boundary) && size <= len(joined); size++ {
			if bytes.Equal(joined[len(joined)-size:], boundary[:size]) {
				keep = size
			}
		}
		if keep == 0 && len(joined) > 0 && (joined[len(joined)-1] == '\r' || joined[len(joined)-1] == '\n') {
			keep = min(2, len(joined))
		}
		if field {
			value = append(value, joined[:len(joined)-keep]...)
		}
		carry = bytes.Clone(joined[len(joined)-keep:])
	}
}

func samlFormDecode(data string) string {
	// http-types parseSimpleQuery splits on '&', allows literal ';', and
	// leaves malformed percent escapes literal. Go ParseQuery rejects them.
	var value strings.Builder
	for index := 0; index < len(data); index++ {
		switch data[index] {
		case '+':
			value.WriteByte(' ')
		case '%':
			if index+2 < len(data) {
				decoded, err := url.QueryUnescape(data[index : index+3])
				if err == nil {
					value.WriteString(decoded)
					index += 2
					continue
				}
			}
			value.WriteByte('%')
		default:
			value.WriteByte(data[index])
		}
	}
	return value.String()
}

func parseSAMLForm(r *http.Request) (url.Values, error) {
	values := url.Values{}
	contentType := r.Header.Get("Content-Type")
	media, _, _ := strings.Cut(contentType, ";")
	if media == "application/x-www-form-urlencoded" {
		data, err := io.ReadAll(io.LimitReader(r.Body, samlFormLimit+1))
		if err != nil {
			return nil, err
		}
		if len(data) > samlFormLimit {
			return nil, errSAMLFormLimit
		}
		for _, parameter := range strings.Split(string(data), "&") {
			if parameter == "" {
				continue
			}
			name, value, _ := strings.Cut(parameter, "=")
			name, value = samlFormDecode(name), samlFormDecode(value)
			if !utf8.ValidString(name) || !utf8.ValidString(value) {
				return nil, errSAMLFormLimit
			}
			values.Add(name, value)
		}
		return values, nil
	}
	_, params, err := mime.ParseMediaType(contentType)
	boundary, exists := params["boundary"]
	if media != "multipart/form-data" || err != nil || !exists {
		return nil, errSAMLFormType
	}
	source := samlFormSource{body: r.Body}
	files, size := 0, 0
	for {
		if _, err := source.line(); errors.Is(err, io.EOF) {
			return values, nil
		} else if err != nil {
			return nil, err
		}
		headers := map[string]string{}
		for count := 0; ; count++ {
			if count > 32 {
				return nil, errSAMLFormLimit
			}
			line, err := source.line()
			if errors.Is(err, io.EOF) {
				return values, nil
			}
			if err != nil {
				return nil, err
			}
			if len(line) == 0 {
				break
			}
			name, value, _ := strings.Cut(string(line), ":")
			name = strings.ToLower(name)
			if _, exists := headers[name]; !exists {
				headers[name] = strings.TrimLeft(value, " ")
			}
		}
		if len(headers) == 0 {
			return values, nil
		}
		_, attributes, _ := mime.ParseMediaType(headers["content-disposition"])
		name, named := attributes["name"]
		_, file := attributes["filename"]
		if named && len(name) > 32 {
			return nil, errSAMLFormLimit
		}
		if named && file {
			if files >= 10 {
				return nil, errSAMLFormLimit
			}
			files++
		}
		value, found, err := source.part([]byte("--"+boundary), named && !file)
		if err != nil {
			return nil, err
		}
		if named && !file {
			size += len(name) + len(value)
			if size > samlFormLimit || !utf8.ValidString(name) || !utf8.Valid(value) {
				return nil, errSAMLFormLimit
			}
			values.Add(name, string(value))
		}
		if !found {
			return values, nil
		}
	}
}
