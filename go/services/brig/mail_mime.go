// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"bytes"
	"encoding/hex"
	"errors"
	"mime/multipart"
	"mime/quotedprintable"
	"net/textproto"
	"strings"
	"unicode"
	"unicode/utf8"
)

type brigMail struct {
	from, fromName, to   string
	toName               *string
	subject, plain, html string
	headers              [][2]string
}

func (brigMail) String() string { return "<BrigMail>" }

func mailHeaderText(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, value)
}

// Native display names use one UTF-8 Q encoded word, including ASCII names.
// Keep native escaping and length decisions instead of a different word fold.
func mailEncodedWord(value string) string {
	var out strings.Builder
	out.WriteString("=?utf-8?Q?")
	for _, b := range []byte(value) {
		switch {
		case b == ' ':
			out.WriteByte('_')
		case b >= 33 && b <= 126 && !strings.ContainsRune(`_?="(),.:;<>@[]\`, rune(b)):
			out.WriteByte(b)
		default:
			out.WriteByte('=')
			out.WriteString(strings.ToUpper(hex.EncodeToString([]byte{b})))
		}
	}
	out.WriteString("?=")
	return out.String()
}

func mailAddress(email string, name *string, limit bool) string {
	address := "<" + mailHeaderText(email) + ">"
	if name == nil {
		return address
	}
	named := mailEncodedWord(*name) + " " + address
	if limit && utf8.RuneCountInString(named) > 320 {
		return address
	}
	return named
}

func (m brigMail) mime() ([]byte, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, part := range []struct{ media, text string }{{"text/plain", m.plain}, {"text/html", m.html}} {
		header := textproto.MIMEHeader{"Content-Type": {part.media + "; charset=utf-8"}, "Content-Transfer-Encoding": {"quoted-printable"}}
		destination, err := writer.CreatePart(header)
		if err != nil {
			return nil, errors.New("mail encoding failed")
		}
		encoder := quotedprintable.NewWriter(destination)
		if _, err := encoder.Write([]byte(strings.ReplaceAll(part.text, "\r", ""))); err != nil {
			return nil, errors.New("mail encoding failed")
		}
		if err := encoder.Close(); err != nil {
			return nil, errors.New("mail encoding failed")
		}
	}
	if writer.Close() != nil {
		return nil, errors.New("mail encoding failed")
	}
	var raw bytes.Buffer
	raw.WriteString("From: " + mailAddress(m.from, &m.fromName, false) + "\r\n")
	raw.WriteString("To: " + mailAddress(m.to, m.toName, true) + "\r\n")
	headers := append([][2]string{{"Subject", m.subject}}, m.headers...)
	for _, header := range headers {
		name := strings.Map(func(r rune) rune {
			if r < 33 || r > 126 || r == ':' {
				return -1
			}
			return r
		}, header[0])
		value := mailHeaderText(header[1])
		if !isMailASCII(value) {
			value = mailEncodedWord(value)
		}
		raw.WriteString(name + ": " + value + "\r\n")
	}
	raw.WriteString("MIME-Version: 1.0\r\nContent-Type: multipart/alternative; boundary=\"" + writer.Boundary() + "\"\r\n\r\n")
	raw.Write(body.Bytes())
	return raw.Bytes(), nil
}

func isMailASCII(value string) bool {
	for _, r := range value {
		if r > 127 {
			return false
		}
	}
	return true
}
