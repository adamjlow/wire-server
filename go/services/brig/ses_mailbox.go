// SPDX-License-Identifier: AGPL-3.0-only
// Display-name grammar adapted from libs/types-common/src/Data/Mailbox.hs.
// Copyright (C) 2025 Wire Swiss GmbH <opensource@wire.com>.
package brig

import "unicode"

// SES uses native Mailbox, not just EmailAddress. Keep display-name parsing
// separate from the existing email-validate addr-spec grammar.
func sesMailboxAddress(value string) (string, error) {
	p := registrationEmailParser{input: value}
	start := p.offset
	if sesMailboxWord(&p) {
		for {
			if sesMailboxWord(&p) {
				continue
			}
			if p.peek() == '.' {
				p.offset++
				continue
			}
			if sesMailboxCFWS(&p) {
				continue
			}
			break
		}
	} else {
		p.offset = start
	}
	sesMailboxCFWS(&p)
	if p.peek() == '<' {
		p.offset++
		address, err := parseRegistrationEmail(&p, '>')
		if err == nil {
			p.offset++
			sesMailboxCFWS(&p)
			if p.offset == len(value) {
				return address, nil
			}
		}
	}
	// Native name-addr <|> addr-spec backtracks the complete first branch.
	return canonicalRegistrationEmail(value)
}

func sesMailboxWord(p *registrationEmailParser) bool {
	start := p.offset
	sesMailboxCFWS(p)
	atomStart := p.offset
	for p.offset < len(p.input) && (emailAtom(p.peek()) || unicode.IsNumber(rune(p.peek()))) {
		p.offset++
	}
	if p.offset > atomStart {
		sesMailboxCFWS(p)
		return true
	}
	p.offset = start
	sesMailboxCFWS(p)
	if p.peek() != '"' {
		p.offset = start
		return false
	}
	p.offset++
	for p.offset < len(p.input) {
		p.fws()
		c := p.peek()
		if c == '"' {
			p.offset++
			return true
		}
		if c == '\\' {
			p.offset++
			if !sesMailboxQuotedPair(p) {
				break
			}
			continue
		}
		if !(c == 33 || c >= 35 && c <= 91 || c >= 93 && c <= 126 || emailNoWhitespaceControl(c)) {
			break
		}
		p.offset++
	}
	p.offset = start
	return false
}

func sesMailboxQuotedPair(p *registrationEmailParser) bool {
	if p.offset >= len(p.input) {
		return false
	}
	c := p.peek()
	if !(c >= 33 && c <= 126 || c == ' ' || c == '\t') {
		return false
	}
	p.offset++
	return true
}

func sesMailboxCFWS(p *registrationEmailParser) bool {
	start := p.offset
	comments := 0
	for {
		before := p.offset
		p.fws()
		if !sesMailboxComment(p) {
			p.offset = before
			break
		}
		comments++
	}
	if comments > 0 {
		p.fws()
		return true
	}
	p.offset = start
	return p.fws()
}

func sesMailboxComment(p *registrationEmailParser) bool {
	start := p.offset
	if p.peek() != '(' {
		return false
	}
	p.offset++
	depth := 1
	for p.offset < len(p.input) {
		// Native comments allow FWS before content, not before the closing ')'.
		space := p.fws()
		c := p.peek()
		if c == ')' {
			if space {
				break
			}
			p.offset++
			depth--
			if depth == 0 {
				return true
			}
			continue
		}
		if c == '(' {
			p.offset++
			depth++
			continue
		}
		if c == '\\' {
			p.offset++
			if !sesMailboxQuotedPair(p) {
				break
			}
			continue
		}
		if !(c >= 33 && c <= 39 || c >= 42 && c <= 91 || c >= 93 && c <= 126 || emailNoWhitespaceControl(c)) {
			break
		}
		p.offset++
	}
	p.offset = start
	return false
}
