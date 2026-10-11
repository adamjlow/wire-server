// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"math"
	"net"
	"net/textproto"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

type smtpMailConfig struct {
	endpoint    Endpoint
	mode        string
	credentials *smtpMailCredentials
}
type smtpMailCredentials struct{ username, passwordFile string }

func (smtpMailConfig) String() string      { return "<SMTPMailConfig>" }
func (smtpMailCredentials) String() string { return "<SMTPMailCredentials>" }

func parseSMTPMailConfig(raw any) (smtpMailConfig, error) {
	var result smtpMailConfig
	failure := errors.New("invalid SMTP configuration")
	fields, ok := raw.(map[string]any)
	if !ok {
		return result, failure
	}
	endpoint, ok := fields["smtpEndpoint"].(map[string]any)
	if !ok {
		return result, failure
	}
	host, ok := endpoint["host"].(string)
	if !ok {
		return result, failure
	}
	port, ok := smtpNativePort(endpoint["port"])
	if !ok {
		return result, failure
	}
	mode, ok := fields["smtpConnType"].(string)
	if !ok || (mode != "plain" && mode != "tls" && mode != "ssl") {
		return result, failure
	}
	result.endpoint = Endpoint{Host: host, Port: port}
	result.mode = mode
	if value := fields["smtpCredentials"]; value != nil {
		creds, ok := value.(map[string]any)
		if !ok {
			return result, failure
		}
		username, ok := creds["smtpUsername"].(string)
		if !ok {
			return result, failure
		}
		path, ok := creds["smtpPassword"].(string)
		if !ok {
			return result, failure
		}
		result.credentials = &smtpMailCredentials{username: username, passwordFile: path}
	}
	return result, nil
}
func readSMTPPassword(path string) (string, error) {
	failure := errors.New("SMTP credentials unavailable")
	raw, err := os.ReadFile(path)
	if err != nil || !utf8.Valid(raw) {
		return "", failure
	}
	var document yaml.Node
	if yaml.Unmarshal(raw, &document) != nil || len(document.Content) != 1 {
		return "", failure
	}
	value := document.Content[0]
	if value.Kind != yaml.ScalarNode || value.Tag != "!!str" {
		return "", failure
	}
	return value.Value, nil
}

type smtpMailConnection struct {
	net    net.Conn
	wire   *textproto.Conn
	idleAt time.Time
}
type smtpMailSender struct {
	config             smtpMailConfig
	password, hostname string
	tlsConfig          *tls.Config
	mu                 sync.Mutex
	idle               []*smtpMailConnection
	slots              chan struct{}
	stopped, done      chan struct{}
	closeOnce          sync.Once
}

func (*smtpMailSender) String() string { return "<SMTPMailSender>" }

func newSMTPMailSender(ctx context.Context, c smtpMailConfig) (*smtpMailSender, error) {
	return newSMTPMailSenderRoots(ctx, c, nil)
}

// Production uses system roots. The explicit root dependency also permits a
// synthetic trusted CA in protocol tests without bypassing TLS verification.
func newSMTPMailSenderRoots(ctx context.Context, c smtpMailConfig, roots *x509.CertPool) (*smtpMailSender, error) {
	host, err := os.Hostname()
	if err != nil || strings.ContainsAny(host, "\r\n") {
		return nil, errors.New("SMTP hostname unavailable")
	}
	s := &smtpMailSender{config: c, hostname: host, tlsConfig: &tls.Config{ServerName: c.endpoint.Host, RootCAs: roots}, slots: make(chan struct{}, 5), stopped: make(chan struct{}), done: make(chan struct{})}
	if c.credentials != nil {
		s.password, err = readSMTPPassword(c.credentials.passwordFile)
		if err != nil {
			return nil, err
		}
	}
	// Native startup probes connect/auth/QUIT before creating its resource pool.
	conn, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	if err = s.action(ctx, conn, func() error { return conn.command("QUIT", 1, 221) }); err != nil {
		_ = conn.net.Close()
		return nil, errors.New("SMTP startup close failed")
	}
	_ = conn.net.Close()
	go s.reap()
	return s, nil
}

func (s *smtpMailSender) action(ctx context.Context, c *smtpMailConnection, run func() error) error {
	// STARTTLS replaces c.net during this action. Capture the original socket
	// so cancellation cannot race with that replacement; both wrappers use it.
	socket := c.net
	deadline := time.Now().Add(15 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if socket.SetDeadline(deadline) != nil {
		return errors.New("SMTP connection unavailable")
	}
	interrupted := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { _ = socket.SetDeadline(time.Now()); close(interrupted) })
	err := run()
	if !stop() {
		<-interrupted
	}
	_ = socket.SetDeadline(time.Time{})
	if err != nil || ctx.Err() != nil {
		return errors.New("SMTP action failed")
	}
	return nil
}

func (c *smtpMailConnection) command(command string, tries int, accepted ...int) error {
	for attempt := 0; attempt < tries; attempt++ {
		if err := c.wire.PrintfLine("%s", command); err != nil {
			return err
		}
		code, _, err := c.wire.ReadResponse(0)
		if err != nil {
			return err
		}
		for _, want := range accepted {
			if code == want {
				return nil
			}
		}
	}
	return errors.New("SMTP command rejected")
}

func (s *smtpMailSender) open(ctx context.Context) (*smtpMailConnection, error) {
	connectionCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	dialer := net.Dialer{}
	var conn net.Conn
	var err error
	if s.config.mode == "ssl" {
		conn, err = (&tls.Dialer{NetDialer: &dialer, Config: s.tlsConfig}).DialContext(connectionCtx, "tcp", s.config.endpoint.Address())
	} else {
		conn, err = dialer.DialContext(connectionCtx, "tcp", s.config.endpoint.Address())
	}
	if err != nil {
		return nil, errors.New("SMTP connection failed")
	}
	c := &smtpMailConnection{net: conn, wire: textproto.NewConn(conn)}
	err = s.action(connectionCtx, c, func() error {
		code, _, err := c.wire.ReadResponse(0)
		if err != nil || code != 220 {
			return errors.New("SMTP greeting rejected")
		}
		if s.config.mode == "tls" {
			// HaskellNet-SSL sends HELO/EHLO/STARTTLS, then a fresh EHLO over TLS.
			if c.command("HELO "+s.hostname, 1, 250, 502) != nil || c.command("EHLO "+s.hostname, 1, 250) != nil || c.command("STARTTLS", 1, 220) != nil {
				return errors.New("SMTP TLS negotiation failed")
			}
			secured := tls.Client(conn, s.tlsConfig)
			if secured.HandshakeContext(connectionCtx) != nil {
				return errors.New("SMTP TLS handshake failed")
			}
			c.net, c.wire = secured, textproto.NewConn(secured)
		}
		return c.command("EHLO "+s.hostname, 3, 250)
	})
	if err == nil && s.config.credentials != nil {
		err = s.action(ctx, c, func() error {
			// Native LOGIN ignores the first two reply codes, checking final235.
			for _, command := range []string{"AUTH LOGIN", base64.StdEncoding.EncodeToString([]byte(s.config.credentials.username))} {
				if err := c.wire.PrintfLine("%s", command); err != nil {
					return err
				}
				if _, _, err := c.wire.ReadResponse(0); err != nil {
					return err
				}
			}
			return c.command(base64.StdEncoding.EncodeToString([]byte(s.password)), 1, 235)
		})
	}
	if err != nil {
		_ = c.net.Close()
		return nil, errors.New("SMTP initialization failed")
	}
	return c, nil
}

func (s *smtpMailSender) send(ctx context.Context, mail brigMail) error {
	if s == nil || s.slots == nil {
		return errors.New("SMTP delivery unavailable")
	}
	if mail.to == "" || strings.ContainsAny(mail.from+mail.to, "\r\n") {
		return errors.New("invalid SMTP envelope")
	}
	raw, err := mail.mime()
	if err != nil {
		return err
	}
	select {
	case s.slots <- struct{}{}:
	case <-ctx.Done():
		return errors.New("SMTP delivery canceled")
	case <-s.stopped:
		return errors.New("SMTP delivery closed")
	}
	defer func() { <-s.slots }()
	s.mu.Lock()
	select {
	case <-s.stopped:
		s.mu.Unlock()
		return errors.New("SMTP delivery closed")
	default:
	}
	var conn *smtpMailConnection
	if count := len(s.idle); count > 0 {
		conn = s.idle[count-1]
		s.idle = s.idle[:count-1]
	}
	s.mu.Unlock()
	if conn == nil {
		conn, err = s.open(ctx)
		if err != nil {
			return err
		}
	}
	err = s.action(ctx, conn, func() error {
		if err := conn.command("MAIL FROM:<"+mail.from+">", 1, 250, 251); err != nil {
			return err
		}
		if err := conn.command("RCPT TO:<"+mail.to+">", 1, 250, 251); err != nil {
			return err
		}
		if err := conn.command("DATA", 1, 354); err != nil {
			return err
		}
		// Preserve HaskellNet0.6.2 line framing. It does not dot-stuff DATA;
		// this inherited SMTP injection concern is recorded for separate hardening.
		lines := bytes.Split(raw, []byte{'\n'})
		if len(lines) > 0 && len(lines[len(lines)-1]) == 0 {
			lines = lines[:len(lines)-1]
		}
		for _, line := range lines {
			line = bytes.TrimSuffix(line, []byte{'\r'})
			if err := conn.wire.PrintfLine("%s", line); err != nil {
				return err
			}
		}
		return conn.command(".", 1, 250, 251)
	})
	if err != nil {
		_ = conn.net.Close()
		return errors.New("SMTP delivery failed")
	}
	s.mu.Lock()
	select {
	case <-s.stopped:
		s.mu.Unlock()
		s.retire(conn)
	default:
		conn.idleAt = time.Now()
		s.idle = append(s.idle, conn)
		s.mu.Unlock()
	}
	return nil
}

func (s *smtpMailSender) retire(conn *smtpMailConnection) {
	_ = s.action(context.Background(), conn, func() error { return conn.command("QUIT", 1, 221) })
	_ = conn.net.Close()
}
func (s *smtpMailSender) reap() {
	defer close(s.done)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopped:
			return
		case at := <-ticker.C:
			var expired []*smtpMailConnection
			s.mu.Lock()
			kept := s.idle[:0]
			for _, conn := range s.idle {
				if at.Sub(conn.idleAt) >= 5*time.Second {
					expired = append(expired, conn)
				} else {
					kept = append(kept, conn)
				}
			}
			s.idle = kept
			s.mu.Unlock()
			var retiring sync.WaitGroup
			for _, conn := range expired {
				retiring.Go(func() { s.retire(conn) })
			}
			retiring.Wait()
		}
	}
}
func (s *smtpMailSender) close() {
	if s == nil {
		return
	}
	s.closeOnce.Do(func() {
		s.mu.Lock()
		close(s.stopped)
		idle := s.idle
		s.idle = nil
		s.mu.Unlock()
		var retiring sync.WaitGroup
		for _, conn := range idle {
			retiring.Go(func() { s.retire(conn) })
		}
		retiring.Wait()
		<-s.done
	})
}

// Native Endpoint.port is a Word16; YAML integer-valued Scientific numbers
// are accepted by its JSON parser, including decimal/exponent notation.
func smtpNativePort(value any) (int, bool) {
	switch number := value.(type) {
	case int:
		return number, number >= 0 && number <= 65535
	case uint64:
		if number <= 65535 {
			return int(number), true
		}
	case float64:
		if number >= 0 && number <= 65535 && number == math.Trunc(number) {
			return int(number), true
		}
	}
	return 0, false
}
