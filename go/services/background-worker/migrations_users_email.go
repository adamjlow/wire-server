// SPDX-License-Identifier: AGPL-3.0-only
// Storage grammar adapted from email-validate-2.3.2.21 Text.Email.Parser.
// Copyright (c) 2009 George Pollard.
// Original BSD3 notice is retained in third_party/email-validate/LICENSE.
package backgroundworker

import "strings"

type migrationEmailParser struct {
	input  string
	offset int
}

func canonicalUserMigrationEmail(value string) (string, error) {
	p := migrationEmailParser{input: value}
	var local []string
	for {
		if !p.cfws() {
			return "", invalidUserMigrationRow()
		}
		part := ""
		if p.peek() == '"' {
			var valid bool
			part, valid = p.quoted()
			if !valid {
				return "", invalidUserMigrationRow()
			}
		} else {
			start := p.offset
			for p.offset < len(value) && emailAtom(value[p.offset]) {
				p.offset++
			}
			if p.offset == start {
				return "", invalidUserMigrationRow()
			}
			part = value[start:p.offset]
		}
		if !p.cfws() {
			return "", invalidUserMigrationRow()
		}
		local = append(local, part)
		if p.peek() != '.' {
			break
		}
		p.offset++
	}
	l := strings.Join(local, ".")
	if len(l) > 64 || p.peek() != '@' {
		return "", invalidUserMigrationRow()
	}
	p.offset++
	if !p.cfws() {
		return "", invalidUserMigrationRow()
	}
	domain := ""
	if p.peek() == '[' {
		p.offset++
		var literal strings.Builder
		for p.offset < len(value) && p.peek() != ']' {
			if p.fws() {
				continue
			}
			c := p.peek()
			if !(c >= 33 && c <= 90 || c >= 94 && c <= 126 || emailNoWhitespaceControl(c)) {
				return "", invalidUserMigrationRow()
			}
			literal.WriteByte(c)
			p.offset++
		}
		if p.peek() != ']' {
			return "", invalidUserMigrationRow()
		}
		p.offset++
		domain = "[" + literal.String() + "]"
	} else {
		var labels []string
		for {
			if !p.cfws() {
				return "", invalidUserMigrationRow()
			}
			start := p.offset
			if !emailAlphaNum(p.peek()) {
				return "", invalidUserMigrationRow()
			}
			p.offset++
			for p.offset < len(value) && (emailAlphaNum(p.peek()) || p.peek() == '-') {
				p.offset++
			}
			label := value[start:p.offset]
			if len(label) > 63 || label[len(label)-1] == '-' {
				return "", invalidUserMigrationRow()
			}
			labels = append(labels, label)
			if !p.cfws() {
				return "", invalidUserMigrationRow()
			}
			if p.peek() != '.' {
				break
			}
			p.offset++
			// Native optional terminal dot is discarded. A following comment
			// or whitespace belongs to a label only when another label exists.
			if p.offset == len(value) {
				break
			}
		}
		domain = strings.Join(labels, ".")
		if len(domain) > 253 {
			return "", invalidUserMigrationRow()
		}
	}
	if !p.cfws() || p.offset != len(value) || len(l)+len(domain)+1 > 254 {
		return "", invalidUserMigrationRow()
	}
	return l + "@" + domain, nil
}

func (p *migrationEmailParser) peek() byte {
	if p.offset >= len(p.input) {
		return 0
	}
	return p.input[p.offset]
}
func emailAlphaNum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}
func emailAtom(c byte) bool {
	return emailAlphaNum(c) || strings.ContainsRune("!#$%&'*+/=?^_`{|}~-", rune(c))
}
func emailNoWhitespaceControl(c byte) bool {
	return c >= 1 && c <= 8 || c >= 11 && c <= 12 || c >= 14 && c <= 31 || c == 127
}

func (p *migrationEmailParser) fws() bool {
	start := p.offset
	for {
		spaces := p.offset
		for p.offset < len(p.input) && (p.peek() == ' ' || p.peek() == '\t') {
			p.offset++
		}
		if p.offset+2 < len(p.input) && p.input[p.offset:p.offset+2] == "\r\n" && (p.input[p.offset+2] == ' ' || p.input[p.offset+2] == '\t') {
			p.offset += 2
			continue
		}
		if spaces == p.offset {
			break
		}
		break
	}
	return p.offset > start
}

func (p *migrationEmailParser) cfws() bool {
	for {
		if p.fws() {
			continue
		}
		if p.peek() != '(' {
			return true
		}
		p.offset++
		depth := 1
		for depth > 0 {
			if p.offset >= len(p.input) {
				return false
			}
			if p.fws() {
				continue
			}
			c := p.peek()
			p.offset++
			switch c {
			case '(':
				depth++
			case ')':
				depth--
			case '\\':
				if p.offset >= len(p.input) || p.peek() > 127 {
					return false
				}
				p.offset++
			default:
				if !(c >= 33 && c <= 39 || c >= 42 && c <= 91 || c >= 93 && c <= 126 || emailNoWhitespaceControl(c)) {
					return false
				}
			}
		}
	}
}

func (p *migrationEmailParser) quoted() (string, bool) {
	p.offset++
	var value strings.Builder
	value.WriteByte('"')
	for p.offset < len(p.input) {
		if p.fws() {
			continue
		}
		c := p.peek()
		p.offset++
		if c == '"' {
			value.WriteByte(c)
			return value.String(), true
		}
		if c == '\\' {
			if p.offset >= len(p.input) || p.peek() > 127 {
				return "", false
			}
			value.WriteByte(c)
			value.WriteByte(p.peek())
			p.offset++
			continue
		}
		if !(c == 33 || c >= 35 && c <= 91 || c >= 93 && c <= 126 || emailNoWhitespaceControl(c)) {
			return "", false
		}
		value.WriteByte(c)
	}
	return "", false
}
