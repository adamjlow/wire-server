// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func dheBotServer(t *testing.T, executable string, certificate tls.Certificate) string {
	t.Helper()
	dir := t.TempDir()
	cert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Certificate[0]})
	key := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(certificate.PrivateKey.(*rsa.PrivateKey))})
	for name, data := range map[string][]byte{"cert.pem": cert, "key.pem": key} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	cmd := exec.Command(executable, "s_server", "-accept", address, "-cert", filepath.Join(dir, "cert.pem"), "-key", filepath.Join(dir, "key.pem"), "-tls1_2", "-cipher", botDHECiphers, "-www")
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = output.Close(); _ = cmd.Wait() })
	ready := make(chan bool, 1)
	go func() {
		scanner := bufio.NewScanner(output)
		for scanner.Scan() {
			if scanner.Text() == "ACCEPT" {
				ready <- true
				_, _ = io.Copy(io.Discard, output)
				return
			}
		}
		ready <- false
	}()
	select {
	case ok := <-ready:
		if !ok {
			t.Fatal("finite-DHE fixture did not start")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("finite-DHE fixture startup timed out")
	}
	return address
}

func TestBotFiniteDHEAuthenticatesActualStream(t *testing.T) {
	executable, err := exec.LookPath("openssl")
	if err != nil {
		t.Fatal("OpenSSL fixture executable required")
	}
	certificate, leaf := syntheticBotCertificate(t, false)
	address := dheBotServer(t, executable, certificate)
	pin := botRSAFingerprint(leaf.PublicKey.(*rsa.PublicKey))
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	// Verify the fixture actually rejects native Go negotiation, then exercise
	// the real fallback through net/http on the same authenticated connection.
	if conn, err := (&tls.Dialer{Config: pinnedBotTLS([][]byte{pin}, nil)}).DialContext(ctx, "tcp", address); err == nil {
		conn.Close()
		t.Fatal("fixture accepts Go cipher suites; finite-DHE gap not exercised")
	}
	transport := &http.Transport{DialTLSContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return dialPinnedBot(ctx, network, address, pinnedBotTLS([][]byte{pin}, nil), executable)
	}}
	defer transport.CloseIdleConnections()
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://"+address+"/", nil)
	response, err := (&http.Client{Transport: transport}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal("authenticated finite-DHE HTTP response changed")
	}
	if _, err := io.Copy(io.Discard, response.Body); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		pin  []byte
		cert tls.Certificate
	}{
		{"wrong-pin", make([]byte, 32), certificate},
		{"expired", pin, func() tls.Certificate { cert, _ := syntheticBotCertificate(t, true); return cert }()},
	} {
		t.Run(test.name, func(t *testing.T) {
			address := address
			pin := test.pin
			if test.name == "expired" {
				address = dheBotServer(t, executable, test.cert)
				leaf, _ := x509.ParseCertificate(test.cert.Certificate[0])
				pin = botRSAFingerprint(leaf.PublicKey.(*rsa.PublicKey))
			}
			conn, err := dialPinnedBot(ctx, "tcp", address, pinnedBotTLS([][]byte{pin}, nil), executable)
			if conn != nil {
				conn.Close()
				t.Fatal("unauthenticated stream returned to HTTP caller")
			}
			var authentication botPinError
			if !errors.As(err, &authentication) {
				t.Fatal("finite-DHE authentication failure was not preserved")
			}
		})
	}
}

func TestBotDHECancellationReapsHelper(t *testing.T) {
	executable, err := exec.LookPath("openssl")
	if err != nil {
		t.Fatal(err)
	}
	// A peer that accepts TCP but never completes TLS must not retain a helper.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	peer := make(chan net.Conn, 1)
	go func() { conn, _ := listener.Accept(); peer <- conn }()
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() {
		_, err := dialBotDHE(ctx, listener.Addr().String(), pinnedBotTLS(nil, nil), executable)
		result <- err
	}()
	select {
	case conn := <-peer:
		if conn == nil {
			t.Fatal("fixture accept failed")
		}
		defer conn.Close()
		cancel()
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("helper never connected")
	}
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("handshake cancellation did not propagate")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled helper was not closed and reaped")
	}
}

func TestBotDHETranscriptRejectsMissingOrOversizedHandshake(t *testing.T) {
	for _, value := range [][]byte{[]byte("SSL-Session:\n    Cipher    : (NONE)\n    Verify return code: 0 (ok)\n---\n\n"), bytes.Repeat([]byte("not a certificate\n"), 100000), []byte("-----BEGIN CERTIFICATE-----\ninvalid\n-----END CERTIFICATE-----\n")} {
		if _, err := readBotDHEHandshake(bufio.NewReader(strings.NewReader(string(value)))); err == nil {
			t.Fatal("invalid handshake accepted")
		}
	}
}

func TestBotGoAuthenticationFailureNeverFallsBack(t *testing.T) {
	certificate, _ := syntheticBotCertificate(t, false)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("unauthenticated connection transmitted HTTP")
	}))
	server.TLS = &tls.Config{Certificates: []tls.Certificate{certificate}}
	server.StartTLS()
	defer server.Close()
	conn, err := dialPinnedBot(t.Context(), "tcp", strings.TrimPrefix(server.URL, "https://"), pinnedBotTLS([][]byte{make([]byte, 32)}, nil), filepath.Join(t.TempDir(), "absent-helper"))
	if conn != nil {
		conn.Close()
		t.Fatal("wrong-pin connection returned")
	}
	var pin botPinError
	if !errors.As(err, &pin) {
		t.Fatal("Go authentication failure was replaced by a helper attempt")
	}
}

func TestBotDHETranscriptBudgetAcceptsLongSubjectLine(t *testing.T) {
	_, leaf := syntheticBotCertificate(t, false)
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leaf.Raw})
	value := strings.Repeat("synthetic-subject", 1000) + "\n" + string(certificate) + "SSL-Session:\n    Cipher    : DHE-RSA-AES128-GCM-SHA256\n    Verify return code: 18 (self-signed certificate)\n---\nHTTP/1.1 204 No Content\r\n\r\n"
	reader := bufio.NewReader(strings.NewReader(value))
	certificates, err := readBotDHEHandshake(reader)
	if err != nil || len(certificates) != 1 || !bytes.Equal(certificates[0].Raw, leaf.Raw) {
		t.Fatal("valid long subject acquired an incidental buffer limit")
	}
	line, err := reader.ReadString('\n')
	if err != nil || line != "HTTP/1.1 204 No Content\r\n" {
		t.Fatal("handshake consumed application data")
	}
}
