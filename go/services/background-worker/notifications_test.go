// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

func ptr[T any](v T) *T { return &v }
func sourceNotification(body string) notification {
	return notification{Origin: ptr("origin.example.com"), Component: ptr("brig"), Path: ptr("/on-user-deleted-connections"), Body: ptr(body), RequestID: ptr("N/A")}
}
func encode(t *testing.T, value any) []byte {
	t.Helper()
	b, e := json.Marshal(value)
	if e != nil {
		t.Fatal(e)
	}
	return b
}

func testFederator(t *testing.T, handler http.HandlerFunc) (*Service, *http.Client) {
	t.Helper()
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		handler(w, r)
	}))
	server.Config.Protocols = new(http.Protocols)
	server.Config.Protocols.SetHTTP1(true)
	server.Config.Protocols.SetUnencryptedHTTP2(true)
	server.Start()
	t.Cleanup(server.Close)
	host, p, _ := net.SplitHostPort(server.Listener.Addr().String())
	port, _ := strconv.Atoi(p)
	c := Config{Federator: Endpoint{host, port}}
	c.Pusher.Min = 10
	c.Pusher.Max = 1000
	s := New(c, GalleyConfig{})
	client := notificationClient()
	t.Cleanup(client.CloseIdleConnections)
	return s, client
}

func TestLegacyNotificationPreservesRawBodyAndV0(t *testing.T) {
	const body = " { \"example\" : [1, 2] } "
	s, client := testFederator(t, func(w http.ResponseWriter, r *http.Request) {
		got, _ := io.ReadAll(r.Body)
		if r.ProtoMajor != 2 || r.URL.Path != "/rpc/target.example.com/brig/on-user-deleted-connections" || r.Header.Get("Wire-Origin-Domain") != "origin.example.com" || r.Header.Get("X-Wire-API-Version") != "0" || r.Header.Get("Request-Id") != "N/A" || string(got) != body {
			t.Errorf("notification changed: %s %v %q", r.URL.Path, r.Header, got)
		}
		_, _ = io.WriteString(w, "{}")
	})
	n := sourceNotification(body)
	n.RequestID = nil
	n.Origin = ptr("Origin.EXAMPLE.com")
	action, err := s.deliverNotification(t.Context(), client, "Target.EXAMPLE.com", encode(t, n))
	if err != nil || action != notificationACK {
		t.Fatalf("%v %v", action, err)
	}
}

func TestBundlesChooseLatestVersionAndLastTie(t *testing.T) {
	var version, body string
	s, client := testFederator(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "api-version") {
			if r.Header.Get("X-Wire-API-Version") != "0" {
				t.Error("version probe must use the native client's numeric V0 default")
			}
			_, _ = io.WriteString(w, `{"supported":[0,1],"supported_versions":[1,3,2,99]}`)
			return
		}
		version = r.Header.Get("X-Wire-API-Version")
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		_, _ = io.WriteString(w, "{}")
	})
	first := sourceNotification(`{"first":true}`)
	first.Versions = &versionRange{From: ptr(1), Until: ptr(4)}
	last := sourceNotification(`{"last":true}`)
	last.Versions = &versionRange{From: ptr(2), Until: ptr(4)}
	action, err := s.deliverNotification(t.Context(), client, "target.example.com", encode(t, payloadBundle{Notifications: []notification{first, last}}))
	if err != nil || action != notificationACK || version != "3" || body != *last.Body {
		t.Fatalf("%v %v %s %s", action, err, version, body)
	}
}

func TestUnsupportedBundlePolicyAndMalformedPayloads(t *testing.T) {
	s, client := testFederator(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "api-version") {
			t.Error("unsupported bundle delivered")
		}
		_, _ = io.WriteString(w, `{"supported":[0,1]}`)
	})
	n := sourceNotification("{}")
	n.Versions = &versionRange{From: ptr(1)}
	for _, tc := range []struct {
		policy *string
		want   notificationAction
	}{{nil, notificationHold}, {ptr("keep_queued"), notificationHold}, {ptr("drop_if_unsupported"), notificationDrop}} {
		action, err := s.deliverNotification(t.Context(), client, "target.example.com", encode(t, payloadBundle{Notifications: []notification{n}, Policy: tc.policy}))
		if err != nil || action != tc.want {
			t.Errorf("%v %v", action, err)
		}
	}
	for _, data := range []string{`{`, `{}`, `{"notifications":[]}`, `{"ownDomain":"evil/host","targetComponent":"brig","path":"x","body":"{}"}`, `{"notifications":[],"unsupportedVersionPolicy":"unknown"}`} {
		action, err := s.deliverNotification(t.Context(), client, "target.example.com", []byte(data))
		if err != nil || action != notificationReject {
			t.Errorf("malformed notification was accepted: %s", data)
		}
	}
}

type acknowledgements struct {
	mu      sync.Mutex
	acks    int
	rejects []bool
}

func (a *acknowledgements) Ack(_ uint64, multiple bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if multiple {
		return io.ErrUnexpectedEOF
	}
	a.acks++
	return nil
}
func (a *acknowledgements) Nack(_ uint64, _, _ bool) error { return io.ErrUnexpectedEOF }
func (a *acknowledgements) Reject(_ uint64, requeue bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.rejects = append(a.rejects, requeue)
	return nil
}

func TestConsumerACKRejectRetryAndUnsupportedCounters(t *testing.T) {
	for _, kind := range []string{"legacy", "compatible-drop", "unsupported-drop", "malformed", "bad-request", "retry", "invalid-error"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			s, client := testFederator(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "api-version") {
					if kind == "unsupported-drop" {
						_, _ = io.WriteString(w, `{"supported":[0,1],"supported_versions":[0]}`)
					} else {
						_, _ = io.WriteString(w, `{"supported":[0,1],"supported_versions":[1]}`)
					}
					return
				}
				calls++
				if kind == "invalid-error" && calls == 1 {
					w.WriteHeader(533)
					_, _ = io.WriteString(w, `{"code":533,"label":"federation-remote-error","message":"remote failure","inner":{"code":400,"label":"bad-request","message":"invalid payload"}} trailing invalid JSON`)
					return
				}
				if kind == "bad-request" {
					w.WriteHeader(533)
					_, _ = io.WriteString(w, `{"code":533,"label":"federation-remote-error","message":"remote failure","inner":{"code":400,"label":"bad-request","message":"invalid payload"}}`)
					return
				}
				if kind == "retry" && calls == 1 {
					w.WriteHeader(521)
					_, _ = io.WriteString(w, `{"label":"transport-failed"}`)
					return
				}
				_, _ = io.WriteString(w, "{}")
			})
			n := sourceNotification("{}")
			data := encode(t, n)
			if kind == "malformed" {
				data = []byte(`{"bad":true}`)
			}
			if strings.HasSuffix(kind, "-drop") {
				n.Versions = &versionRange{From: ptr(1)}
				data = encode(t, payloadBundle{Notifications: []notification{n}, Policy: ptr("drop_if_unsupported")})
			}
			acks := &acknowledgements{}
			deliveries := make(chan amqp.Delivery, 1)
			deliveries <- amqp.Delivery{Body: data, DeliveryTag: 1, Acknowledger: acks}
			close(deliveries)
			s.consumeNotifications(t.Context(), client, "target.example.com", deliveries)
			if kind == "malformed" || kind == "bad-request" {
				if acks.acks != 0 || len(acks.rejects) != 1 || acks.rejects[0] {
					t.Fatalf("wrong rejection: %+v", acks)
				}
			} else if acks.acks != 1 || len(acks.rejects) != 0 {
				t.Fatalf("wrong acknowledgement: %+v", acks)
			}
			m := s.notifications["target.example.com"]
			if kind == "unsupported-drop" && (m == nil || m.dropped != 1 || m.pushed != 0) {
				t.Fatal("unsupported drop counted as delivery")
			}
			if kind == "compatible-drop" && (m == nil || m.pushed != 1 || m.dropped != 0) {
				t.Fatal("compatible drop policy did not count delivery")
			}
			if (kind == "retry" || kind == "invalid-error") && (calls != 2 || m.errors != 1 || m.pushed != 1 || m.stuck) {
				t.Fatal("delivery failure was not retried")
			}
			if kind == "bad-request" && (m == nil || m.errors != 1 || m.stuck) {
				t.Fatal("terminal rejection left queue stuck")
			}
		})
	}
}

func TestKeepQueuedShutdownNeverACKsUnsentMessage(t *testing.T) {
	s, client := testFederator(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"supported":[0,1],"supported_versions":[0]}`)
	})
	n := sourceNotification("{}")
	n.Versions = &versionRange{From: ptr(1)}
	acks := &acknowledgements{}
	deliveries := make(chan amqp.Delivery, 1)
	deliveries <- amqp.Delivery{Body: encode(t, payloadBundle{Notifications: []notification{n}}), Acknowledger: acks, DeliveryTag: 1}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	s.consumeNotifications(ctx, client, "target.example.com", deliveries)
	if acks.acks != 0 || len(acks.rejects) != 0 {
		t.Fatal("unsent delivery removed from queue")
	}
}

func TestRabbitHandshakeRespectsCancellation(t *testing.T) {
	t.Setenv("RABBITMQ_USERNAME", "synthetic-user")
	t.Setenv("RABBITMQ_PASSWORD", "synthetic-password")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		c, e := listener.Accept()
		if e == nil {
			accepted <- c
		}
	}()
	host, p, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(p)
	s := New(Config{Rabbit: RabbitConfig{Endpoint: Endpoint{host, port}}}, GalleyConfig{})
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	c, err := s.openRabbit(ctx, "test")
	if c != nil {
		_ = c.Close()
	}
	select {
	case peer := <-accepted:
		peer.Close()
	default:
	}
	if err == nil || time.Since(started) > time.Second {
		t.Fatal("AMQP handshake ignored cancellation")
	}
}

func TestNotificationSuccessRequiresSourceResponseContract(t *testing.T) {
	for _, tc := range []struct {
		name, media, body string
		status            int
		success           bool
	}{
		{"object", "application/json", `{}`, 200, true},
		{"object-fields", "application/json; charset=utf-8", `{"future":true}`, 200, true},
		{"missing-media", "", `{}`, 200, false},
		{"wrong-media", "application/octet-stream", `{}`, 200, false},
		{"wrong-status", "application/json", `{}`, 204, false},
		{"empty-body", "application/json", ``, 200, false},
		{"array-body", "application/json", `[]`, 200, false},
		{"null-body", "application/json", `null`, 200, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, client := testFederator(t, func(w http.ResponseWriter, r *http.Request) {
				if tc.media == "" {
					w.Header().Del("Content-Type")
				} else {
					w.Header().Set("Content-Type", tc.media)
				}
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			})
			_, err := s.deliverNotification(t.Context(), client, "target.example.com", encode(t, sourceNotification("{}")))
			if (err == nil) != tc.success {
				t.Fatalf("response acceptance mismatch: success=%v error=%v", tc.success, err)
			}
		})
	}
}

func TestInvalidNotificationResponseRetriesBeforeACK(t *testing.T) {
	calls := 0
	s, client := testFederator(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Content-Type", "application/octet-stream")
		}
		_, _ = io.WriteString(w, "{}")
	})
	acks := &acknowledgements{}
	deliveries := make(chan amqp.Delivery, 1)
	deliveries <- amqp.Delivery{Body: encode(t, sourceNotification("{}")), DeliveryTag: 1, Acknowledger: acks}
	close(deliveries)
	s.consumeNotifications(t.Context(), client, "target.example.com", deliveries)
	if calls != 2 || acks.acks != 1 || len(acks.rejects) != 0 {
		t.Fatalf("invalid response acknowledged or not retried: calls=%d ack=%+v", calls, acks)
	}
}
