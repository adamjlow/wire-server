// SPDX-License-Identifier: AGPL-3.0-or-later
package proxy

import (
	"context"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

var errSecrets = errors.New("invalid proxy secrets configuration")
var numberSyntax = regexp.MustCompile(`^[+-]?[0-9]+(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$`)

type Secrets struct {
	mu     sync.RWMutex
	values map[string]any
	path   string
}

func LoadSecrets(path string) (*Secrets, error) {
	values, err := readSecretFile(path, "", map[string]any{}, map[string]bool{})
	if err != nil {
		return nil, errSecrets
	}
	return &Secrets{values: values, path: path}, nil
}

func (s *Secrets) get(name string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.values["secrets."+name].(string)
	return value, ok
}

// Source autoReload retains the previous map on error and checks size/mtime
// each 60 seconds. No secret value or parser input appears in an error/log.
func (s *Secrets) Watch(ctx context.Context, onError func()) {
	meta := func() string {
		info, err := os.Stat(s.path)
		if err != nil {
			return "missing"
		}
		return strconv.FormatInt(info.Size(), 10) + ":" + strconv.FormatInt(info.ModTime().Unix(), 10)
	}
	previous := meta()
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			next := meta()
			if next == previous {
				continue
			}
			previous = next
			updated, err := LoadSecrets(s.path)
			if err != nil {
				onError()
				continue
			}
			s.mu.Lock()
			s.values = updated.values
			s.mu.Unlock()
		}
	}
}

type secretParser struct {
	text   []rune
	pos    int
	file   string
	values map[string]any
	stack  map[string]bool
}

func readSecretFile(file, prefix string, values map[string]any, stack map[string]bool) (map[string]any, error) {
	path, err := interpolate(file, "", map[string]any{})
	if err != nil || stack[path] || len(stack) > 128 {
		return nil, errSecrets
	}
	contents, err := os.ReadFile(path)
	if err != nil || !utf8.Valid(contents) {
		return nil, errSecrets
	}
	stack[path] = true
	defer delete(stack, path)
	p := secretParser{text: []rune(string(contents)), file: file, values: values, stack: stack}
	if err := p.directives(prefix, false, 0); err != nil {
		return nil, errSecrets
	}
	return values, nil
}

func (p *secretParser) peek() rune {
	if p.pos >= len(p.text) {
		return 0
	}
	return p.text[p.pos]
}

func (p *secretParser) space(all bool) {
	for p.pos < len(p.text) {
		c := p.peek()
		if c == '#' {
			for p.pos < len(p.text) && p.peek() != '\r' && p.peek() != '\n' {
				p.pos++
			}
		} else if (all && unicode.IsSpace(c)) || c == ' ' || c == '\t' {
			p.pos++
		} else {
			break
		}
	}
}

func (p *secretParser) quoted() (string, error) {
	if p.peek() != '"' {
		return "", errSecrets
	}
	p.pos++
	var value strings.Builder
	for p.pos < len(p.text) {
		c := p.peek()
		p.pos++
		if c == '"' {
			return value.String(), nil
		}
		if c != '\\' {
			value.WriteRune(c)
			continue
		}
		c = p.peek()
		p.pos++
		switch c {
		case 'n':
			value.WriteByte('\n')
		case 'r':
			value.WriteByte('\r')
		case 't':
			value.WriteByte('\t')
		case '"', '\\':
			value.WriteRune(c)
		case 'u':
			readQuad := func() (rune, error) {
				if p.pos+4 > len(p.text) {
					return 0, errSecrets
				}
				n, err := strconv.ParseUint(string(p.text[p.pos:p.pos+4]), 16, 16)
				p.pos += 4
				return rune(n), err
			}
			r, err := readQuad()
			if err != nil {
				return "", errSecrets
			}
			if utf16.IsSurrogate(r) {
				if p.pos+2 > len(p.text) || string(p.text[p.pos:p.pos+2]) != `\u` {
					return "", errSecrets
				}
				p.pos += 2
				r2, err := readQuad()
				if err != nil || r > 0xdbff || r2 < 0xdc00 || r2 > 0xdfff {
					return "", errSecrets
				}
				r = utf16.DecodeRune(r, r2)
			}
			value.WriteRune(r)
		default:
			return "", errSecrets
		}
	}
	return "", errSecrets
}

func (p *secretParser) value(depth int) (any, error) {
	if depth > 128 {
		return nil, errSecrets
	}
	if p.peek() == '"' {
		return p.quoted()
	}
	if p.peek() == '[' {
		p.pos++
		p.space(true)
		var values []any
		for p.peek() != ']' {
			value, err := p.value(depth + 1)
			if err != nil {
				return nil, errSecrets
			}
			values = append(values, value)
			p.space(true)
			if p.peek() == ']' {
				break
			}
			if p.peek() != ',' {
				return nil, errSecrets
			}
			p.pos++
			p.space(true)
			if p.peek() == ']' {
				return nil, errSecrets
			}
		}
		p.pos++
		return values, nil
	}
	start := p.pos
	for p.pos < len(p.text) && !unicode.IsSpace(p.peek()) && !strings.ContainsRune("#,]}", p.peek()) {
		p.pos++
	}
	token := string(p.text[start:p.pos])
	switch token {
	case "on", "true":
		return true, nil
	case "off", "false":
		return false, nil
	}
	if numberSyntax.MatchString(token) {
		if number, ok := new(big.Rat).SetString(token); ok {
			return number, nil
		}
	}
	return nil, errSecrets
}

func (p *secretParser) directives(prefix string, closing bool, depth int) error {
	if depth > 128 {
		return errSecrets
	}
	for {
		p.space(true)
		if closing && p.peek() == '}' {
			p.pos++
			return nil
		}
		if p.pos == len(p.text) && !closing {
			return nil
		}
		if !unicode.IsLetter(p.peek()) {
			return errSecrets
		}
		start := p.pos
		p.pos++
		for unicode.IsLetter(p.peek()) || unicode.IsDigit(p.peek()) || p.peek() == '_' || p.peek() == '-' {
			p.pos++
		}
		name := string(p.text[start:p.pos])
		p.space(true)
		if name == "import" {
			file, err := p.quoted()
			if err != nil {
				return errSecrets
			}
			if !filepath.IsAbs(file) {
				file = filepath.Join(filepath.Dir(p.file), file)
			}
			if _, err := readSecretFile(file, prefix, p.values, p.stack); err != nil {
				return errSecrets
			}
		} else if p.peek() == '{' {
			p.pos++
			if p.directives(prefix+name+".", true, depth+1) != nil {
				return errSecrets
			}
		} else if p.peek() == '=' {
			p.pos++
			p.space(true)
			value, err := p.value(0)
			if err != nil {
				return errSecrets
			}
			if raw, ok := value.(string); ok {
				value, err = interpolate(raw, prefix, p.values)
				if err != nil {
					return errSecrets
				}
			}
			p.values[prefix+name] = value
		} else {
			return errSecrets
		}
		p.space(false)
		if p.pos < len(p.text) && p.peek() != '\r' && p.peek() != '\n' && !(closing && p.peek() == '}') {
			return errSecrets
		}
	}
}

func interpolate(value, prefix string, values map[string]any) (string, error) {
	var out strings.Builder
	for i := 0; i < len(value); i++ {
		if value[i] != '$' {
			out.WriteByte(value[i])
			continue
		}
		i++
		if i >= len(value) {
			return "", errSecrets
		}
		if value[i] == '$' {
			out.WriteByte('$')
			continue
		}
		if value[i] != '(' {
			return "", errSecrets
		}
		end := strings.IndexByte(value[i+1:], ')')
		if end <= 0 {
			return "", errSecrets
		}
		name := value[i+1 : i+1+end]
		i += end + 1
		var found any
		for scope := prefix; ; {
			if candidate, ok := values[scope+name]; ok {
				found = candidate
				break
			}
			if scope == "" {
				break
			}
			parts := strings.Split(strings.TrimSuffix(scope, "."), ".")
			scope = ""
			if len(parts) > 1 {
				scope = strings.Join(parts[:len(parts)-1], ".") + "."
			}
		}
		switch found := found.(type) {
		case string:
			out.WriteString(found)
		case *big.Rat:
			if found.IsInt() {
				out.WriteString(found.Num().String())
			} else {
				f, _ := found.Float64()
				out.WriteString(strconv.FormatFloat(f, 'g', -1, 64))
			}
		case nil:
			environment, ok := os.LookupEnv(name)
			if !ok {
				return "", errSecrets
			}
			out.WriteString(environment)
		default:
			return "", errSecrets
		}
	}
	return out.String(), nil
}
