// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type smtpFixture struct {
	tlsHold                     <-chan struct{}
	tlsRequested                chan struct{}
	ehloFailures                int32
	listener                    net.Listener
	mode                        string
	tlsConfig                   *tls.Config
	roots                       *x509.CertPool
	connected, quit, auth, ehlo atomic.Int32
	rejectAuth, rejectMail      bool
	hold                        <-chan struct{}
	received                    chan string
	wg                          sync.WaitGroup
	closed                      chan struct{}
}

func newSMTPFixture(t *testing.T, mode string, setup ...func(*smtpFixture)) *smtpFixture {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &smtpFixture{listener: listener, mode: mode, received: make(chan string, 20), closed: make(chan struct{})}
	if mode != "plain" {
		// httptest's synthetic certificate supplies a valid127.0.0.1 SAN/CA.
		certificateSource := httptest.NewTLSServer(http.NotFoundHandler())
		f.tlsConfig = &tls.Config{Certificates: certificateSource.TLS.Certificates}
		f.roots = x509.NewCertPool()
		f.roots.AddCert(certificateSource.Certificate())
		certificateSource.Close()
		if mode == "ssl" {
			f.listener = tls.NewListener(listener, f.tlsConfig)
		}
	}
	for _, initialize := range setup {
		initialize(f)
	}
	go func() {
		defer close(f.closed)
		for {
			conn, err := f.listener.Accept()
			if err != nil {
				return
			}
			f.connected.Add(1)
			f.wg.Go(func() { f.serve(conn) })
		}
	}()
	t.Cleanup(func() { _ = f.listener.Close(); <-f.closed; f.wg.Wait() })
	return f
}
func (f *smtpFixture) config() smtpMailConfig {
	host, port, _ := net.SplitHostPort(f.listener.Addr().String())
	number, _ := strconv.Atoi(port)
	return smtpMailConfig{endpoint: Endpoint{Host: host, Port: number}, mode: f.mode}
}
func (f *smtpFixture) serve(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	wire := textproto.NewConn(conn)
	if wire.PrintfLine("220 synthetic SMTP") != nil {
		return
	}
	for {
		line, err := wire.ReadLine()
		if err != nil {
			return
		}
		switch {
		case strings.HasPrefix(line, "HELO "):
			_ = wire.PrintfLine("250 synthetic hello")
		case strings.HasPrefix(line, "EHLO "):
			count := f.ehlo.Add(1)
			if count <= f.ehloFailures {
				_ = wire.PrintfLine("554 synthetic EHLO rejection")
				continue
			}
			_ = wire.PrintfLine("250-synthetic hello")
			_ = wire.PrintfLine("250 AUTH LOGIN")
		case line == "STARTTLS":
			_ = wire.PrintfLine("220 start TLS")
			if f.tlsHold != nil {
				f.tlsRequested <- struct{}{}
				<-f.tlsHold
			}
			secured := tls.Server(conn, f.tlsConfig)
			if secured.Handshake() != nil {
				return
			}
			conn = secured
			wire = textproto.NewConn(conn)
		case line == "AUTH LOGIN":
			_ = wire.PrintfLine("334 VXNlcm5hbWU6")
			username, err := wire.ReadLine()
			if err != nil {
				return
			}
			_ = wire.PrintfLine("334 UGFzc3dvcmQ6")
			password, err := wire.ReadLine()
			if err != nil {
				return
			}
			if username != base64.StdEncoding.EncodeToString([]byte("synthetic-user")) || password != base64.StdEncoding.EncodeToString([]byte("synthetic password\n")) || f.rejectAuth {
				_ = wire.PrintfLine("535 rejected")
				return
			}
			f.auth.Add(1)
			_ = wire.PrintfLine("235 accepted")
		case strings.HasPrefix(line, "MAIL FROM:"):
			if f.rejectMail {
				_ = wire.PrintfLine("554 synthetic private rejection")
			} else {
				_ = wire.PrintfLine("251 accepted")
			}
		case strings.HasPrefix(line, "RCPT TO:"):
			_ = wire.PrintfLine("251 accepted")
		case line == "DATA":
			_ = wire.PrintfLine("354 send data")
			var body strings.Builder
			for {
				line, err = wire.ReadLine()
				if err != nil {
					return
				}
				if line == "." {
					break
				}
				body.WriteString(line)
				body.WriteString("\n")
			}
			f.received <- body.String()
			if f.hold != nil {
				<-f.hold
			}
			_ = wire.PrintfLine("251 queued")
		case line == "QUIT":
			f.quit.Add(1)
			_ = wire.PrintfLine("221 bye")
			return
		default:
			_ = wire.PrintfLine("500 unknown")
		}
	}
}
func smtpSyntheticMail() brigMail {
	return brigMail{from: "sender@example.com", fromName: "Synthetic", to: "recipient@example.com", subject: "Synthetic subject", plain: "synthetic body\n.leading-dot", html: "<p>synthetic</p>"}
}

func TestSMTPActualDeliveryModesAndLOGIN(t *testing.T) {
	for _, mode := range []string{"plain", "tls", "ssl"} {
		t.Run(mode, func(t *testing.T) {
			f := newSMTPFixture(t, mode)
			path := filepath.Join(t.TempDir(), "synthetic-password.yaml")
			if err := os.WriteFile(path, []byte("|\n  synthetic password\n"), 0600); err != nil {
				t.Fatal(err)
			}
			config := f.config()
			config.credentials = &smtpMailCredentials{username: "synthetic-user", passwordFile: path}
			sender, err := newSMTPMailSenderRoots(context.Background(), config, f.roots)
			if err != nil {
				t.Fatal(err)
			}
			defer sender.close()
			if f.connected.Load() != 1 || f.quit.Load() != 1 || f.auth.Load() != 1 {
				t.Fatal("startup connection/auth/QUIT probe missing")
			}
			for range 2 {
				if err := sender.send(context.Background(), smtpSyntheticMail()); err != nil {
					t.Fatal(err)
				}
				select {
				case body := <-f.received:
					if !strings.Contains(body, "Subject: Synthetic subject") || !strings.Contains(body, "synthetic body") || !strings.Contains(body, "\n.leading-dot\n") {
						t.Fatal("SMTP lost MIME content")
					}
				case <-time.After(time.Second):
					t.Fatal("missing SMTP message")
				}
			}
			if f.connected.Load() != 2 || f.auth.Load() != 2 {
				t.Fatal("authenticated pooled connection not reused")
			}
			sender.close()
			sender.close()
			if f.quit.Load() != 2 {
				t.Fatal("pooled connection not closed gracefully")
			}
			if mode == "tls" && f.ehlo.Load() != 4 {
				t.Fatal("native pre/post STARTTLS greetings changed")
			}
			if sender.String() != "<SMTPMailSender>" {
				t.Fatal("SMTP secret exposure")
			}
		})
	}
}

func TestSMTPPoolBoundAndIdleRetirement(t *testing.T) {
	release := make(chan struct{})
	f := newSMTPFixture(t, "plain", func(f *smtpFixture) { f.hold = release })
	sender, err := newSMTPMailSender(context.Background(), f.config())
	if err != nil {
		t.Fatal(err)
	}
	defer sender.close()
	results := make(chan error, 6)
	for range 6 {
		go func() { results <- sender.send(context.Background(), smtpSyntheticMail()) }()
	}
	for range 5 {
		select {
		case <-f.received:
		case <-time.After(time.Second):
			close(release)
			t.Fatal("five leased deliveries did not reach server")
		}
	}
	if f.connected.Load() != 6 {
		close(release)
		t.Fatal("native five resource bound changed")
	}
	select {
	case <-f.received:
		close(release)
		t.Fatal("sixth simultaneous SMTP delivery exceeded pool")
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	for range 6 {
		select {
		case err := <-results:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("queued SMTP send did not complete")
		}
	}
	<-f.received
	// Age owned idle resources to exercise actual reaper without sleeping5s.
	sender.mu.Lock()
	for _, conn := range sender.idle {
		conn.idleAt = time.Now().Add(-6 * time.Second)
	}
	sender.mu.Unlock()
	deadline := time.Now().Add(2 * time.Second)
	for f.quit.Load() != 6 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if f.quit.Load() != 6 {
		t.Fatal("native five second idle retirement missing")
	}
}

func TestSMTPFailuresCancellationAndTLSVerification(t *testing.T) {
	t.Run("untrusted-ssl", func(t *testing.T) {
		f := newSMTPFixture(t, "ssl")
		if _, err := newSMTPMailSender(context.Background(), f.config()); err == nil {
			t.Fatal("untrusted certificate accepted")
		}
	})
	t.Run("untrusted-starttls", func(t *testing.T) {
		f := newSMTPFixture(t, "tls")
		if _, err := newSMTPMailSender(context.Background(), f.config()); err == nil {
			t.Fatal("untrusted STARTTLS certificate accepted")
		}
	})
	t.Run("mail-rejection", func(t *testing.T) {
		f := newSMTPFixture(t, "plain", func(f *smtpFixture) { f.rejectMail = true })
		sender, err := newSMTPMailSender(context.Background(), f.config())
		if err != nil {
			t.Fatal(err)
		}
		defer sender.close()
		err = sender.send(context.Background(), smtpSyntheticMail())
		if err == nil || strings.Contains(err.Error(), "private") {
			t.Fatal("SMTP rejection lost or private details leaked")
		}
		if f.connected.Load() != 2 {
			t.Fatal("rejected send unexpectedly retried")
		}
	})
	t.Run("cancellation", func(t *testing.T) {
		release := make(chan struct{})
		f := newSMTPFixture(t, "plain", func(f *smtpFixture) { f.hold = release })
		defer close(release)
		sender, err := newSMTPMailSender(context.Background(), f.config())
		if err != nil {
			t.Fatal(err)
		}
		defer sender.close()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		result := make(chan error, 1)
		go func() { result <- sender.send(ctx, smtpSyntheticMail()) }()
		select {
		case <-f.received:
		case <-time.After(time.Second):
			t.Fatal("send not started")
		}
		cancel()
		select {
		case err := <-result:
			if err == nil {
				t.Fatal("canceled send succeeded")
			}
		case <-time.After(time.Second):
			t.Fatal("SMTP cancellation did not interrupt socket")
		}
	})
	t.Run("missing-envelope", func(t *testing.T) {
		f := newSMTPFixture(t, "plain")
		sender, err := newSMTPMailSender(context.Background(), f.config())
		if err != nil {
			t.Fatal(err)
		}
		defer sender.close()
		if sender.send(context.Background(), brigMail{}) == nil {
			t.Fatal("missing recipient succeeded")
		}
		if f.connected.Load() != 1 {
			t.Fatal("invalid envelope touched delivery connection")
		}
	})
}

func TestSMTPNativeGreetingAndAuthenticationFailures(t *testing.T) {
	for _, count := range []int32{2, 3} {
		t.Run(strconv.Itoa(int(count)), func(t *testing.T) {
			f := newSMTPFixture(t, "plain", func(f *smtpFixture) { f.ehloFailures = count })
			sender, err := newSMTPMailSender(context.Background(), f.config())
			if count == 2 {
				if err != nil {
					t.Fatal(err)
				}
				sender.close()
			} else if err == nil {
				sender.close()
				t.Fatal("EHLO rejection accepted")
			}
			if f.ehlo.Load() != 3 {
				t.Fatal("native EHLO three-attempt budget changed")
			}
		})
	}
	f := newSMTPFixture(t, "plain", func(f *smtpFixture) { f.rejectAuth = true })
	path := filepath.Join(t.TempDir(), "synthetic-password.yaml")
	if err := os.WriteFile(path, []byte("|\n  synthetic password\n"), 0600); err != nil {
		t.Fatal(err)
	}
	config := f.config()
	config.credentials = &smtpMailCredentials{username: "synthetic-user", passwordFile: path}
	if sender, err := newSMTPMailSender(context.Background(), config); err == nil {
		sender.close()
		t.Fatal("startup LOGIN rejection accepted")
	}
}

func TestSMTPNativeConfigAndSecretText(t *testing.T) {
	fields := map[string]any{"smtpEndpoint": map[string]any{"host": "localhost", "port": 2525}, "smtpConnType": "plain", "smtpCredentials": map[string]any{"smtpUsername": "synthetic-user", "smtpPassword": "synthetic-file"}}
	config, err := parseSMTPMailConfig(fields)
	if err != nil || config.credentials.username != "synthetic-user" {
		t.Fatal("native SMTP config rejected")
	}
	fields["smtpConnType"] = "TLS"
	if _, err := parseSMTPMailConfig(fields); err == nil {
		t.Fatal("non-native mode accepted")
	}
	path := filepath.Join(t.TempDir(), "secret.yaml")
	for _, sample := range []struct {
		yaml, want string
		bad        bool
	}{{"'123'", "123", false}, {"|\n  synthetic\n", "synthetic\n", false}, {"123", "", true}, {"null", "", true}, {"[synthetic]", "", true}} {
		if err := os.WriteFile(path, []byte(sample.yaml), 0600); err != nil {
			t.Fatal(err)
		}
		value, err := readSMTPPassword(path)
		if (err != nil) != sample.bad || value != sample.want {
			t.Fatal("native YAML Text secret grammar changed")
		}
	}
	if _, err := readSMTPPassword(path + ".absent"); err == nil || errors.Is(err, os.ErrNotExist) {
		t.Fatal("missing secret accepted or private filesystem error exposed")
	}
}

func TestSMTPSTARTTLSCancellationDuringHandshake(t *testing.T) {
	release, requested := make(chan struct{}), make(chan struct{}, 1)
	f := newSMTPFixture(t, "tls", func(f *smtpFixture) { f.tlsHold = release; f.tlsRequested = requested })
	defer close(release)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		sender, err := newSMTPMailSenderRoots(ctx, f.config(), f.roots)
		if sender != nil {
			sender.close()
		}
		result <- err
	}()
	select {
	case <-requested:
	case <-time.After(time.Second):
		t.Fatal("TLS upgrade not reached")
	}
	cancel()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("canceled STARTTLS initialization succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("canceled STARTTLS handshake did not release connection")
	}
}
