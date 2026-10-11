// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/go-stomp/stomp/v3/frame"
	"go.yaml.in/yaml/v3"
)

func stompTestBroker(t *testing.T, serve func(net.Conn)) *internalStompQueue {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		c, err := listener.Accept()
		if err == nil {
			defer c.Close()
			_ = c.SetDeadline(time.Now().Add(3 * time.Second))
			serve(c)
		}
	}()
	t.Cleanup(func() { _ = listener.Close(); <-done })
	addr := listener.Addr().(*net.TCPAddr)
	return &internalStompQueue{host: "127.0.0.1", port: addr.Port, destination: "/queue/brig", user: "test:operator", pass: "synthetic\\password"}
}

func stompTestHandshake(t *testing.T, c net.Conn) *frame.Reader {
	t.Helper()
	r := bufio.NewReader(c)
	raw, err := r.ReadString(0)
	if err != nil || !strings.Contains(raw, "login:test:operator\n") || !strings.Contains(raw, "passcode:synthetic\\password\n") {
		t.Error("CONNECT must preserve raw credential characters")
		return nil
	}
	if _, err = io.WriteString(c, "CONNECTED\nversion:1.2\nheart-beat:0,0\n\n\x00"); err != nil {
		t.Error(err)
		return nil
	}
	return frame.NewReader(r)
}

func TestInternalStompPersistentReceipt(t *testing.T) {
	q := stompTestBroker(t, func(c net.Conn) {
		r := stompTestHandshake(t, c)
		if r == nil {
			return
		}
		f, err := r.Read()
		if err != nil {
			t.Error(err)
			return
		}
		if f.Command != frame.SEND || f.Header.Get("persistent") != "true" || f.Header.Get(frame.Destination) != "/queue/brig" || f.Header.Get(frame.ContentType) != "application/json" || string(f.Body) != "{\"type\":\"user.delete\"}" {
			t.Error("persistent send contract mismatch")
		}
		_ = frame.NewWriter(c).Write(frame.New(frame.RECEIPT, frame.ReceiptId, f.Header.Get(frame.Receipt)))
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := q.send(ctx, []byte(`{"type":"user.delete"}`)); err != nil {
		t.Fatal(err)
	}
}

func TestInternalStompUnconfirmedSendFails(t *testing.T) {
	q := stompTestBroker(t, func(c net.Conn) {
		r := stompTestHandshake(t, c)
		if r == nil {
			return
		}
		f, err := r.Read()
		if err != nil {
			t.Error(err)
			return
		}
		_ = frame.NewWriter(c).Write(frame.New(frame.RECEIPT, frame.ReceiptId, "wrong-"+f.Header.Get(frame.Receipt)))
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if q.send(ctx, []byte(`{}`)) == nil {
		t.Fatal("mismatched receipt accepted")
	}
}

func TestInternalStompAcknowledgementAfterCallback(t *testing.T) {
	for _, success := range []bool{true, false} {
		t.Run(strconv.FormatBool(success), func(t *testing.T) {
			called := make(chan struct{})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			q := stompTestBroker(t, func(c net.Conn) {
				r := stompTestHandshake(t, c)
				if r == nil {
					return
				}
				sub, err := r.Read()
				if err != nil {
					t.Error(err)
					return
				}
				if sub.Header.Get(frame.Ack) != "client-individual" {
					t.Error("wrong acknowledgement mode")
				}
				message := frame.New(frame.MESSAGE, frame.Subscription, "/queue/brig", frame.MessageId, "m1", frame.Ack, "ack1")
				message.Body = []byte(`{"type":"user.delete","user":"00000000-0000-4000-8000-000000000001"}`)
				if frame.NewWriter(c).Write(message) != nil {
					return
				}
				ack, err := r.Read()
				if success {
					if err != nil || ack.Command != frame.ACK || ack.Header.Get(frame.Id) != "ack1" {
						t.Error("missing individual acknowledgement")
					}
					select {
					case <-called:
					default:
						t.Error("ack preceded callback")
					}
					cancel()
				} else if err == nil {
					t.Error("failed callback acknowledged")
				}
			})
			p := internalDeletionProcessor{deleteUser: func(context.Context, storedUser, string) error { return nil }, get: func(context.Context, gocql.UUID) (*storedUser, error) {
				close(called)
				if !success {
					return nil, errors.New("synthetic failure")
				}
				return nil, nil
			}}
			err := q.consume(ctx, p)
			if err == nil {
				t.Fatal("consumer terminated without cancellation or failure")
			}
		})
	}
}

func TestInternalStompRejectsUntrustedTLS(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("STOMP TLS verification bypassed") }))
	defer server.Close()
	address := server.Listener.Addr().(*net.TCPAddr)
	q := internalStompQueue{host: "127.0.0.1", port: address.Port, tls: true, destination: "synthetic", user: "synthetic", pass: "synthetic"}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if q.send(ctx, []byte(`{}`)) == nil {
		t.Fatal("untrusted broker certificate accepted")
	}
}

func TestInternalStompFiveAdditionalAttempts(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan int, 1)
	go func() {
		attempts := 0
		defer func() { done <- attempts }()
		for attempts < 6 {
			c, err := listener.Accept()
			if err != nil {
				return
			}
			attempts++
			_ = c.SetDeadline(time.Now().Add(time.Second))
			r := stompTestHandshake(t, c)
			if r != nil {
				_, _ = r.Read()
				_ = frame.NewWriter(c).Write(frame.New(frame.ERROR, "message", "synthetic broker refusal"))
			}
			_ = c.Close()
		}
	}()
	address := listener.Addr().(*net.TCPAddr)
	q := internalStompQueue{host: "127.0.0.1", port: address.Port, destination: "/queue/brig", user: "test:operator", pass: "synthetic\\password"}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	if q.enqueue(ctx, []byte(`{}`)) == nil {
		t.Fatal("broker refusal accepted")
	}
	_ = listener.Close()
	if attempts := <-done; attempts != 6 {
		t.Fatalf("native attempts: got %d, want 6", attempts)
	}
}

func TestInternalStompBoundsAndMalformedFrames(t *testing.T) {
	for _, raw := range []string{"MESSAGE\ncontent-length:4294967295\n\n", "MESSAGE\ncontent-length:-1\n\n", "MESSAGE\n" + strings.Repeat("a", 5000) + "\n\n", "MESSAGE\ncontent-length:1\n\nx!", "MESSAGE\n\n" + strings.Repeat("x", 4<<20+2)} {
		s := internalStompSession{reader: bufio.NewReader(strings.NewReader(raw))}
		if _, err := s.read(); err == nil {
			t.Fatal("unsafe frame accepted")
		}
	}
	s := internalStompSession{reader: bufio.NewReader(strings.NewReader("MESSAGE\ncontent-length:3\n\na\x00b\x00"))}
	if f, err := s.read(); err != nil || string(f.Body) != "a\x00b" {
		t.Fatal("bounded binary frame was not preserved")
	}
}

func TestInternalStompIdleCancellation(t *testing.T) {
	connected := make(chan struct{})
	q := stompTestBroker(t, func(c net.Conn) {
		r := stompTestHandshake(t, c)
		if r == nil {
			return
		}
		if _, err := r.Read(); err != nil {
			return
		}
		close(connected)
		_, _ = r.Read()
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = q.consume(ctx, internalDeletionProcessor{}) }()
	<-connected
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("idle STOMP read ignored cancellation")
	}
}

func TestInternalStompNativeConfiguration(t *testing.T) {
	var c Config
	if err := yaml.Unmarshal([]byte("stomp:\n  stomHost: localhost\n  stomPort: 61613\n  stomTls: false\n"), &c); err != nil {
		t.Fatal(err)
	}
	var bad Config
	if yaml.Unmarshal([]byte("stomp:\n  stompHost: localhost\n  stompPort: 61613\n  stompTls: false\n"), &bad) == nil {
		t.Fatal("example keys substituted for native parser contract")
	}
	path := filepath.Join(t.TempDir(), "credentials.yaml")
	if os.WriteFile(path, []byte("user: synthetic\npass: synthetic\n"), 0600) != nil {
		t.Fatal("fixture write failed")
	}
	c.Settings.Other = map[string]any{"setStomp": path}
	if _, err := newInternalStompQueue(c, "/queue/brig"); err != nil {
		t.Fatal(err)
	}
	if _, err := newInternalStompQueue(c, "injected\nqueue"); err == nil {
		t.Fatal("framing injection accepted")
	}
}
