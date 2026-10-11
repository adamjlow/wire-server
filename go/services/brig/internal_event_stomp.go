// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/go-stomp/stomp/v3/frame"
	"go.yaml.in/yaml/v3"
)

// These are the actual native FromJSON keys, including its "stom" prefix.
type internalStompConfig struct {
	Host string `yaml:"stomHost"`
	Port int    `yaml:"stomPort"`
	TLS  bool   `yaml:"stomTls"`
}

func (c *internalStompConfig) UnmarshalYAML(node *yaml.Node) error {
	fields, err := stompNativeFields(node, 0)
	if err != nil {
		return err
	}
	host, hOK := fields["stomHost"].(string)
	tls, tOK := fields["stomTls"].(bool)
	port, pOK := fields["stomPort"].(json.Number)
	if !hOK || !tOK || !pOK {
		return errors.New("invalid internal STOMP configuration")
	}
	n, err := stompNativeInteger(string(port))
	if err != nil {
		return errors.New("invalid internal STOMP configuration")
	}
	c.Host, c.Port, c.TLS = host, n, tls
	return nil
}

type internalStompQueue struct {
	host, destination, user, pass string
	port                          int
	tls                           bool
}

func (internalStompQueue) String() string { return "<InternalStompQueue>" }

func newInternalStompQueue(c Config, destination string) (*internalStompQueue, error) {
	invalid := errors.New("internal STOMP configuration unavailable")
	path, ok := c.Settings.Other["setStomp"].(string)
	if c.Stomp == nil || !ok || path == "" || c.Stomp.Host == "" || c.Stomp.Port < 1 || c.Stomp.Port > 65535 || destination == "" {
		return nil, invalid
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, invalid
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 64<<10+1))
	if err != nil || len(raw) > 64<<10 {
		return nil, invalid
	}
	// Native loadSecret accepts YAML (including JSON), and requires both fields.
	var document yaml.Node
	if yaml.Unmarshal(raw, &document) != nil || len(document.Content) != 1 {
		return nil, invalid
	}
	fields, err := stompNativeFields(document.Content[0], 0)
	if err != nil {
		return nil, invalid
	}
	user, uOK := fields["user"].(string)
	pass, pOK := fields["pass"].(string)
	if !uOK || !pOK {
		return nil, invalid
	}
	for _, value := range []string{c.Stomp.Host, destination, user, pass} {
		if strings.ContainsAny(value, "\x00\r\n") {
			return nil, invalid
		}
	}
	return &internalStompQueue{host: c.Stomp.Host, port: c.Stomp.Port, tls: c.Stomp.TLS, destination: destination, user: user, pass: pass}, nil
}

type internalStompSession struct {
	conn    net.Conn
	reader  *bufio.Reader
	writer  *frame.Writer
	version string
	stop    func() bool
}

func (s *internalStompSession) close() { s.stop(); _ = s.conn.Close() }

func (q *internalStompQueue) connect(ctx context.Context) (*internalStompSession, error) {
	address := net.JoinHostPort(q.host, strconv.Itoa(q.port))
	dialer := &net.Dialer{Timeout: time.Second}
	var conn net.Conn
	var err error
	if q.tls {
		conn, err = (&tls.Dialer{NetDialer: dialer, Config: &tls.Config{ServerName: q.host, MinVersion: tls.VersionTLS12}}).DialContext(ctx, "tcp", address)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", address)
	}
	if err != nil {
		return nil, errors.New("internal STOMP connection failed")
	}
	s := &internalStompSession{conn: conn, reader: bufio.NewReader(conn), writer: frame.NewWriter(conn)}
	s.stop = context.AfterFunc(ctx, func() { _ = conn.Close() })
	deadline := time.Now().Add(time.Second)
	if limit, ok := ctx.Deadline(); ok && limit.Before(deadline) {
		deadline = limit
	}
	_ = conn.SetDeadline(deadline)
	// CONNECT headers are not escaped in STOMP. The library's generic writer
	// escapes all commands, so write these already framing-validated values raw.
	connect := "CONNECT\nhost:" + q.host + "\naccept-version:1.0,1.1,1.2\nlogin:" + q.user + "\npasscode:" + q.pass + "\nheart-beat:0,0\n\n\x00"
	if _, err := io.Copy(conn, strings.NewReader(connect)); err != nil {
		s.close()
		return nil, errors.New("internal STOMP handshake failed")
	}
	response, err := s.read()
	if err != nil || response == nil || response.Command != frame.CONNECTED {
		s.close()
		return nil, errors.New("internal STOMP handshake failed")
	}
	s.version = response.Header.Get(frame.Version)
	if s.version == "" {
		s.version = "1.0"
	}
	if s.version != "1.0" && s.version != "1.1" && s.version != "1.2" {
		s.close()
		return nil, errors.New("internal STOMP version unsupported")
	}
	_ = conn.SetDeadline(time.Time{})
	return s, nil
}

// The upstream frame reader trusts content-length and uses unbounded ReadBytes.
// Validate a bounded frame first, then let the library decode headers and syntax.
// Limits match the existing 4 MiB internal SQS response budget; no broker data
// or protocol error text is included in logs or returned errors.
func (s *internalStompSession) read() (*frame.Frame, error) {
	invalid := errors.New("invalid internal STOMP frame")
	var raw []byte
	length := -1
	for {
		line, err := s.reader.ReadSlice('\n')
		if err != nil || len(raw)+len(line) > 64<<10 {
			return nil, invalid
		}
		raw = append(raw, line...)
		text := strings.TrimSuffix(strings.TrimSuffix(string(line), "\n"), "\r")
		if len(raw) == len(line) && text == "" {
			return nil, nil
		}
		if text == "" {
			break
		}
		if value, ok := strings.CutPrefix(text, "content-length:"); ok && length == -1 {
			n, err := strconv.ParseUint(value, 10, 32)
			if err != nil || n > 4<<20 {
				return nil, invalid
			}
			length = int(n)
		}
	}
	if length >= 0 {
		body := make([]byte, length+1)
		if _, err := io.ReadFull(s.reader, body); err != nil || body[length] != 0 {
			return nil, invalid
		}
		raw = append(raw, body...)
	} else {
		start := len(raw)
		for {
			part, err := s.reader.ReadSlice(0)
			if len(raw)-start+len(part) > 4<<20+1 {
				return nil, invalid
			}
			raw = append(raw, part...)
			if err == nil {
				break
			}
			if err != bufio.ErrBufferFull {
				return nil, invalid
			}
		}
	}
	f, err := frame.NewReader(bytes.NewReader(raw)).Read()
	if err != nil {
		return nil, invalid
	}
	return f, nil
}

var internalStompReceipt atomic.Uint64

func (q *internalStompQueue) enqueue(ctx context.Context, body []byte) error {
	for attempt := 0; ; attempt++ {
		work, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		err := q.send(work, body)
		cancel()
		if err == nil {
			return nil
		}
		if attempt >= 5 || ctx.Err() != nil {
			return err
		}
		if !feedbackWait(ctx, 50*time.Millisecond<<attempt) {
			return errors.New("internal STOMP enqueue canceled")
		}
	}
}

func (q *internalStompQueue) send(ctx context.Context, body []byte) error {
	s, err := q.connect(ctx)
	if err != nil {
		return err
	}
	defer s.close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = s.conn.SetDeadline(deadline)
	}
	receipt := "brig-" + strconv.FormatUint(internalStompReceipt.Add(1), 10)
	f := frame.New(frame.SEND, frame.Destination, q.destination, frame.ContentType, "application/json", frame.ContentLength, strconv.Itoa(len(body)), frame.Receipt, receipt, "persistent", "true")
	f.Body = body
	if s.writer.Write(f) != nil {
		return errors.New("internal STOMP send failed")
	}
	for {
		r, err := s.read()
		if err != nil {
			return errors.New("internal STOMP receipt unavailable")
		}
		if r == nil {
			continue
		}
		if r.Command != frame.RECEIPT || r.Header.Get(frame.ReceiptId) != receipt {
			return errors.New("internal STOMP receipt invalid")
		}
		// The receipt proves broker acceptance; abrupt close is safe here.
		return ctx.Err()
	}
}

func (q *internalStompQueue) listen(ctx context.Context, processor internalDeletionProcessor) {
	for ctx.Err() == nil {
		if q.consume(ctx, processor) != nil && ctx.Err() == nil {
			log.Print("Internal STOMP read or callback failed")
		}
		if !feedbackWait(ctx, time.Second) {
			return
		}
	}
}

func (q *internalStompQueue) consume(ctx context.Context, processor internalDeletionProcessor) error {
	s, err := q.connect(ctx)
	if err != nil {
		return err
	}
	defer s.close()
	_ = s.conn.SetWriteDeadline(time.Now().Add(time.Second))
	if s.writer.Write(frame.New(frame.SUBSCRIBE, frame.Id, q.destination, frame.Destination, q.destination, frame.Ack, "client-individual")) != nil {
		return errors.New("internal STOMP subscription failed")
	}
	_ = s.conn.SetWriteDeadline(time.Time{})
	for {
		message, err := s.read()
		if err != nil {
			return err
		}
		if message == nil {
			continue
		}
		if message.Command != frame.MESSAGE {
			return errors.New("invalid internal STOMP message")
		}
		event, err := decodeInternalDeletionEvent(json.RawMessage(message.Body))
		if err != nil {
			return err
		}
		if err = processor.process(ctx, event); err != nil {
			return err
		}
		ack := frame.New(frame.ACK)
		if s.version == "1.2" {
			id := message.Header.Get(frame.Ack)
			if id == "" {
				return errors.New("internal STOMP acknowledgement missing")
			}
			ack.Header.Set(frame.Id, id)
		} else {
			id := message.Header.Get(frame.MessageId)
			if id == "" {
				return errors.New("internal STOMP acknowledgement missing")
			}
			ack.Header.Set(frame.MessageId, id)
			ack.Header.Set(frame.Subscription, q.destination)
		}
		_ = s.conn.SetWriteDeadline(time.Now().Add(time.Second))
		if s.writer.Write(ack) != nil {
			return errors.New("internal STOMP acknowledgement failed")
		}
		_ = s.conn.SetWriteDeadline(time.Time{})
	}
}
