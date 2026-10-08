package cargohold

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"strconv"
	"strings"
)

type uploadSettings struct {
	Public    bool            `json:"public"`
	Retention string          `json:"retention"`
	ConvID    json.RawMessage `json:"convId"`
	Filename  *string         `json:"filename"`
	Filetype  *string         `json:"filetype"`
}

// Positions restart between the reference's metadata and payload parsers.
// Keep its diagnostic positions without adopting a generic MIME parser, whose
// delimiter/checksum/trailer rules differ from the observed service contract.
type parserPosition struct{ line, column, offset int }

func newPosition() parserPosition { return parserPosition{line: 1, column: 1} }
func (p *parserPosition) advance(data string) {
	for _, c := range []byte(data) {
		p.offset++
		if c == '\n' {
			p.line++
			p.column = 1
		} else {
			p.column++
		}
	}
}
func (p parserPosition) failure(message string) error {
	return fmt.Errorf("Expected: , Failed reading: %s at %d:%d (%d)", message, p.line, p.column, p.offset)
}
func (p *parserPosition) lineFrom(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	p.advance(line)
	if err != nil {
		return "", p.failure("not enough input")
	}
	return strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"), nil
}

func parseUpload(input io.Reader) (*bufio.Reader, uploadSettings, int64, error) {
	r := bufio.NewReader(input)
	var settings uploadSettings
	p := newPosition()
	if first, _ := r.Peek(1); len(first) == 1 && (first[0] == '\r' || first[0] == '\n') {
		if _, err := p.lineFrom(r); err != nil {
			return r, settings, 0, err
		}
	}
	meta, beforeBlank, err := partHeaders(r, &p, false)
	if err != nil {
		return r, settings, 0, err
	}
	typ, _, err := mime.ParseMediaType(meta["content-type"])
	if _, present := meta["content-type"]; !present {
		return r, settings, 0, beforeBlank.failure("Missing Content-Type")
	}
	if err != nil {
		return r, settings, 0, beforeBlank.failure("Invalid MIME type")
	}
	n, err := headerLength(meta, beforeBlank)
	if err != nil {
		return r, settings, 0, err
	}
	if typ != "application/json" {
		return r, settings, 0, p.failure("Invalid metadata Content-Type. Expected 'application/json'.")
	}
	raw, err := io.ReadAll(io.LimitReader(r, n))
	p.advance(string(raw))
	if err != nil || int64(len(raw)) != n {
		return r, settings, 0, p.failure("not enough input")
	}
	if err = json.Unmarshal(raw, &settings); err != nil {
		message := err.Error()
		var syntax *json.SyntaxError
		if errors.As(err, &syntax) {
			start := int(syntax.Offset) - 1
			if start < 0 {
				start = 0
			}
			if start > len(raw) {
				start = len(raw)
			}
			for start > 0 && ((raw[start-1] >= 'a' && raw[start-1] <= 'z') || (raw[start-1] >= 'A' && raw[start-1] <= 'Z')) {
				start--
			}
			tail := string(raw[start:])
			tail = strings.TrimSuffix(tail, "}")
			message = "Unexpected " + strconv.Quote(tail) + ", expecting JSON value"
		}
		return r, settings, 0, p.failure(message)
	}
	p = newPosition()
	line, err := p.lineFrom(r)
	if err != nil {
		return r, settings, 0, err
	}
	if line != "" {
		return r, settings, 0, p.failure("endOfLine")
	}
	headers, beforeBlank, err := partHeaders(r, &p, true)
	if err != nil {
		return r, settings, 0, err
	}
	if _, present := headers["content-type"]; !present {
		return r, settings, 0, beforeBlank.failure("Missing Content-Type")
	}
	if _, _, err = mime.ParseMediaType(headers["content-type"]); err != nil {
		return r, settings, 0, beforeBlank.failure("Invalid MIME type")
	}
	n, err = headerLength(headers, beforeBlank)
	return r, settings, n, err
}

func partHeaders(r *bufio.Reader, p *parserPosition, payload bool) (map[string]string, parserPosition, error) {
	if prefix, _ := r.Peek(2); len(prefix) == 2 && string(prefix) != "--" {
		position := *p
		if prefix[0] == '-' {
			position.column++
			position.offset++
		}
		return nil, position, fmt.Errorf("Expected: MIME boundary, -, Failed reading: satisfyWith at %d:%d (%d)", position.line, position.column, position.offset)
	}
	line, err := p.lineFrom(r)
	if err != nil {
		return nil, *p, err
	}
	if !strings.HasPrefix(line, "--") {
		return nil, *p, p.failure("satisfy")
	}
	headers := map[string]string{}
	for {
		before := *p
		line, err = p.lineFrom(r)
		if err != nil {
			return nil, before, err
		}
		if line == "" {
			return headers, before, nil
		}
		end := 0
		for end < len(line) {
			c := line[end]
			if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
				end++
				continue
			}
			break
		}
		original := line[:end]
		name := strings.ToLower(original)
		_, duplicate := headers[name]
		if duplicate || (name != "content-type" && name != "content-length" && !(payload && name == "content-md5")) {
			before.advance(original)
			return nil, before, before.failure("Unexpected header: " + strconv.Quote(original))
		}
		if end >= len(line) || line[end] != ':' {
			before.advance(original)
			return nil, before, before.failure("satisfy")
		}
		headers[name] = strings.TrimLeft(line[end+1:], " \t")
	}
}
func headerLength(headers map[string]string, p parserPosition) (int64, error) {
	value, present := headers["content-length"]
	if !present {
		return 0, p.failure("Missing Content-Type")
	}
	n, err := lengthPrefix(value)
	if err != nil {
		return 0, p.failure("Failed reading: takeWhile1")
	}
	return n, nil
}
func lengthPrefix(value string) (int64, error) {
	end := 0
	for end < len(value) && value[end] >= '0' && value[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0, fmt.Errorf("invalid content length")
	}
	return strconv.ParseInt(value[:end], 10, 64)
}
