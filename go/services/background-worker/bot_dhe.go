// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const botDHECiphers = "DHE-RSA-AES256-GCM-SHA384:DHE-RSA-AES128-GCM-SHA256:DHE-RSA-CHACHA20-POLY1305"

// Go's TLS stack lacks the finite-field suites accepted by Ssl.Util.rsaCiphers.
// Use the image's trusted OpenSSL executable only after a remote negotiation
// failure, never after authentication failure. It owns no HTTP decisions or data:
// Go verifies the actual connection's certificate before returning a stream to
// net/http. No request credentials appear in arguments, environment or diagnostics.
func dialPinnedBot(ctx context.Context, network, address string, config *tls.Config, openssl string) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	conn, err := (&tls.Dialer{Config: config}).DialContext(ctx, network, address)
	if err == nil {
		return conn, nil
	}
	var pin botPinError
	if errors.As(err, &pin) || ctx.Err() != nil || !strings.HasSuffix(err.Error(), "remote error: tls: handshake failure") {
		return nil, err
	}
	return dialBotDHE(ctx, address, config, openssl)
}

// botProcessConn is a single authenticated TLS stream. Close kills and reaps
// its helper; deadline expiry also closes it, unblocking both pipe directions.
// Helpers never outlive the connection or its initial handshake cancellation.
type botProcessConn struct {
	reader *bufio.Reader
	input  io.WriteCloser
	output io.ReadCloser
	cmd    *exec.Cmd
	once   sync.Once
	mu     sync.Mutex
	timer  *time.Timer
	closed bool
}

func (c *botProcessConn) Read(p []byte) (int, error)  { return c.reader.Read(p) }
func (c *botProcessConn) Write(p []byte) (int, error) { return c.input.Write(p) }
func (c *botProcessConn) Close() error {
	c.once.Do(func() {
		c.mu.Lock()
		c.closed = true
		if c.timer != nil {
			c.timer.Stop()
		}
		c.mu.Unlock()
		_ = c.input.Close()
		_ = c.cmd.Process.Kill()
		_ = c.output.Close()
		_ = c.cmd.Wait()
	})
	return nil
}
func (*botProcessConn) LocalAddr() net.Addr  { return botStreamAddr("local") }
func (*botProcessConn) RemoteAddr() net.Addr { return botStreamAddr("bot") }
func (c *botProcessConn) SetDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return net.ErrClosed
	}
	if c.timer != nil {
		c.timer.Stop()
	}
	if !t.IsZero() {
		c.timer = time.AfterFunc(time.Until(t), func() { _ = c.Close() })
	}
	return nil
}
func (c *botProcessConn) SetReadDeadline(t time.Time) error  { return c.SetDeadline(t) }
func (c *botProcessConn) SetWriteDeadline(t time.Time) error { return c.SetDeadline(t) }

type botStreamAddr string

func (a botStreamAddr) Network() string { return "tcp" }
func (a botStreamAddr) String() string  { return string(a) }

func dialBotDHE(ctx context.Context, address string, config *tls.Config, openssl string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil || host == "" || config.VerifyConnection == nil {
		return nil, errors.New("invalid bot TLS transport")
	}
	// TLS1.2 isolates the unsupported suites; the first Go connection negotiates
	// native ECDHE/TLS1.3. -nocommands prevents application bytes being interpreted
	// as CLI commands. OpenSSL's CA verdict is not our authentication decision:
	// native registered pins deliberately support authenticated self-signatures.
	cmd := exec.Command(openssl, "s_client", "-connect", address, "-servername", host,
		"-tls1_2", "-cipher", botDHECiphers, "-showcerts", "-ign_eof", "-nocommands")
	// Do not pass backend credentials or environment-selected OpenSSL config/
	// provider paths to the helper. The packaged executable supplies its defaults.
	cmd.Env = []string{"LANG=C"}
	input, err := cmd.StdinPipe()
	if err != nil {
		return nil, errors.New("bot TLS helper unavailable")
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		_ = input.Close()
		return nil, errors.New("bot TLS helper unavailable")
	}
	// stderr is discarded, including peer-controlled certificate diagnostics.
	if err := cmd.Start(); err != nil {
		_ = input.Close()
		_ = output.Close()
		return nil, errors.New("bot TLS helper unavailable")
	}
	conn := &botProcessConn{input: input, output: output, cmd: cmd, reader: bufio.NewReader(output)}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	certificates, err := readBotDHEHandshake(conn.reader)
	if err == nil {
		err = config.VerifyConnection(tls.ConnectionState{PeerCertificates: certificates})
	}
	if !stop() || ctx.Err() != nil {
		_ = conn.Close()
		return nil, ctx.Err()
	}
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

// Consume only OpenSSL's pre-application transcript. The certificate list is
// untrusted until verified above; a successful session boundary and finite-DHE
// cipher are required. Bound transcript memory independently of the HTTP body.
func readBotDHEHandshake(r *bufio.Reader) ([]*x509.Certificate, error) {
	var certificates []*x509.Certificate
	var certificate strings.Builder
	inCertificate, session, verify, cipher := false, false, false, false
	remaining := 1 << 20
	for remaining > 0 {
		line, err := botHandshakeLine(r, &remaining)
		if err != nil {
			return nil, errors.New("bot TLS handshake failed")
		}
		value := string(line)
		if value == "-----BEGIN CERTIFICATE-----\n" {
			if inCertificate {
				return nil, botPinError{}
			}
			inCertificate = true
		}
		if inCertificate {
			certificate.Write(line)
			if value == "-----END CERTIFICATE-----\n" {
				block, rest := pem.Decode([]byte(certificate.String()))
				if block == nil || len(rest) != 0 {
					return nil, botPinError{}
				}
				cert, err := x509.ParseCertificate(block.Bytes)
				if err != nil {
					return nil, botPinError{}
				}
				certificates = append(certificates, cert)
				certificate.Reset()
				inCertificate = false
			}
			continue
		}
		if value == "SSL-Session:\n" {
			session = true
		}
		if session && strings.HasPrefix(value, "    Cipher    : ") {
			name := strings.TrimSpace(strings.TrimPrefix(value, "    Cipher    : "))
			for _, allowed := range strings.Split(botDHECiphers, ":") {
				cipher = cipher || name == allowed
			}
		}
		if session && strings.HasPrefix(value, "    Verify return code:") {
			verify = true
		}
		if session && verify && value == "---\n" {
			if !cipher || len(certificates) == 0 {
				return nil, errors.New("bot TLS handshake failed")
			}
			return certificates, nil
		}
	}
	return nil, errors.New("bot TLS handshake failed")
}

// Long subject names printed by OpenSSL must not acquire bufio's incidental
// 4KiB line limit. Charge every fragment against the whole transcript budget.
func botHandshakeLine(r *bufio.Reader, remaining *int) ([]byte, error) {
	var value []byte
	for {
		part, err := r.ReadSlice('\n')
		*remaining -= len(part)
		if *remaining < 0 {
			return nil, errors.New("bot TLS handshake failed")
		}
		if value == nil && err != bufio.ErrBufferFull {
			return part, err
		}
		value = append(value, part...)
		if err != bufio.ErrBufferFull {
			return value, err
		}
	}
}
