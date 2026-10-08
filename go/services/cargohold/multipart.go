package cargohold

import (
	"bufio"
	"encoding/json"
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

// The reference accepts independent delimiter lines and a numeric length prefix.
// MIME's generic multipart parser would change these externally visible rules.
func parseUpload(input io.Reader) (*bufio.Reader, uploadSettings, int64, error) {
	r := bufio.NewReader(input)
	var settings uploadSettings
	if b, _ := r.Peek(1); len(b) == 1 && (b[0] == '\r' || b[0] == '\n') {
		if _, err := readLine(r); err != nil {
			return r, settings, 0, err
		}
	}
	meta, err := partHeaders(r, false)
	if err != nil {
		return r, settings, 0, err
	}
	typ, _, err := mime.ParseMediaType(meta["content-type"])
	if err != nil || typ != "application/json" {
		return r, settings, 0, fmt.Errorf("invalid metadata Content-Type")
	}
	n, err := lengthPrefix(meta["content-length"])
	if err != nil {
		return r, settings, 0, err
	}
	// Decode through a limited stream rather than allocating the declared length.
	limited := &io.LimitedReader{R: r, N: n}
	decoder := json.NewDecoder(limited)
	if err = decoder.Decode(&settings); err != nil {
		return r, settings, 0, err
	}
	var extra any
	if err = decoder.Decode(&extra); err != io.EOF {
		return r, settings, 0, fmt.Errorf("invalid metadata JSON")
	}
	if limited.N != 0 {
		return r, settings, 0, io.ErrUnexpectedEOF
	}
	line, err := readLine(r)
	if err != nil || line != "" {
		return r, settings, 0, fmt.Errorf("missing payload separator")
	}
	headers, err := partHeaders(r, true)
	if err != nil {
		return r, settings, 0, err
	}
	if _, _, err = mime.ParseMediaType(headers["content-type"]); err != nil {
		return r, settings, 0, fmt.Errorf("invalid payload Content-Type")
	}
	n, err = lengthPrefix(headers["content-length"])
	return r, settings, n, err
}

func readLine(r *bufio.Reader) (string, error) {
	s, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(strings.TrimSuffix(s, "\n"), "\r"), nil
}

func partHeaders(r *bufio.Reader, payload bool) (map[string]string, error) {
	line, err := readLine(r)
	if err != nil || !strings.HasPrefix(line, "--") {
		return nil, fmt.Errorf("invalid MIME boundary")
	}
	h := map[string]string{}
	for {
		line, err = readLine(r)
		if err != nil {
			return nil, err
		}
		if line == "" {
			return h, nil
		}
		name, value, ok := strings.Cut(line, ":")
		name = strings.ToLower(name)
		_, duplicate := h[name]
		if !ok || duplicate || (name != "content-type" && name != "content-length" && !(payload && name == "content-md5")) {
			return nil, fmt.Errorf("unexpected MIME header")
		}
		h[name] = strings.TrimLeft(value, " \t")
	}
}

func lengthPrefix(value string) (int64, error) {
	i := 0
	for i < len(value) && value[i] >= '0' && value[i] <= '9' {
		i++
	}
	if i == 0 {
		return 0, fmt.Errorf("invalid content length")
	}
	return strconv.ParseInt(value[:i], 10, 64)
}
