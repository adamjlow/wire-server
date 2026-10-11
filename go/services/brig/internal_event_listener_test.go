// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func TestInternalDeletionNativeEventDecoder(t *testing.T) {
	id := gocql.TimeUUID().String()
	for _, body := range []string{
		`{"type":"user.delete","user":"` + id + `"}`,
		`{"type":"client.delete","user":"` + id + `","client":"000A","connection":null}`,
		`{"type":"client.delete","user":"` + id + `","client":{"id":"000A"},"connection":"synthetic"}`,
		`{"type":"service.delete","provider":"` + id + `","service":"` + id + `"}`,
	} {
		event, err := decodeInternalDeletionEvent(json.RawMessage(body))
		if err != nil || event.kind == "client.delete" && event.client != "a" {
			t.Fatal("native event rejected", event, err)
		}
	}
	for _, body := range []string{`null`, `{}`, `{"Type":"user.delete","user":"` + id + `"}`, `{"type":"user.delete","user":"bad"}`, `{"type":"client.delete","user":"` + id + `","client":"a"}`, `{"type":"client.delete","user":"` + id + `","client":{},"connection":null}`, `{"type":"unknown"}`} {
		if _, err := decodeInternalDeletionEvent(json.RawMessage(body)); err == nil {
			t.Fatal("invalid event accepted", body)
		}
	}
}

func TestInternalDeletionCallbackBeforeAcknowledgement(t *testing.T) {
	for _, mode := range []string{"success", "missing-body", "malformed", "callback-failure", "delete-failure", "missing-user", "unsupported-service"} {
		t.Run(mode, func(t *testing.T) {
			var deletes atomic.Int32
			queue := feedbackTestQueue(t, func(w http.ResponseWriter, r *http.Request) {
				if r.ParseForm() != nil || r.Form.Get("Action") != "DeleteMessage" || r.Form.Get("ReceiptHandle") != "synthetic-receipt" || !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
					t.Error("native signed acknowledgement")
				}
				deletes.Add(1)
				if mode == "delete-failure" {
					w.WriteHeader(503)
					w.Write([]byte(`<ErrorResponse><Error><Code>Unavailable</Code></Error></ErrorResponse>`))
					return
				}
				w.Write([]byte(`<DeleteMessageResponse/>`))
			})
			id := gocql.TimeUUID()
			deleted := int32(2)
			lookups, cleanups := 0, 0
			processor := internalDeletionProcessor{
				get: func(ctx context.Context, user gocql.UUID) (*storedUser, error) {
					lookups++
					deadline, ok := ctx.Deadline()
					if !ok || time.Until(deadline) > 60*time.Second || time.Until(deadline) < 59*time.Second {
						t.Error("native minute deadline")
					}
					if user != id {
						t.Error("wrong user")
					}
					if mode == "missing-user" {
						return nil, nil
					}
					return &storedUser{ID: id, Status: &deleted}, nil
				},
				deleteUser: func(_ context.Context, user storedUser, _ string) error {
					cleanups++
					if *user.Status != 2 {
						t.Error("tombstone not replayed")
					}
					if mode == "callback-failure" {
						return errors.New("synthetic failure")
					}
					return nil
				},
			}
			listener := internalEventListener{queue: queue, processor: processor}
			body := `{"type":"user.delete","user":"` + id.String() + `"}`
			receipt := "synthetic-receipt"
			message := sqsFeedbackMessage{Body: &body, Receipt: &receipt}
			if mode == "missing-body" {
				message.Body = nil
			}
			if mode == "malformed" {
				body = `not JSON`
			}
			if mode == "unsupported-service" {
				body = `{"type":"service.delete","provider":"` + id.String() + `","service":"` + id.String() + `"}`
			}
			err := listener.message(context.Background(), message)
			failed := mode == "callback-failure" || mode == "delete-failure" || mode == "unsupported-service"
			ack := mode == "success" || mode == "missing-user" || mode == "delete-failure"
			if (err != nil) != failed || (deletes.Load() == 1) != ack {
				t.Fatal("ack/failure", err, deletes.Load())
			}
			if mode == "missing-user" && cleanups != 0 {
				t.Fatal("missing account cleanup")
			}
			if mode == "success" && (lookups != 1 || cleanups != 1) {
				t.Fatal("tombstone replay absent")
			}
		})
	}
}

func TestInternalDeletionCanceledDelayAndListenerJoin(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	p := internalDeletionProcessor{get: func(context.Context, gocql.UUID) (*storedUser, error) { cancel(); return nil, nil }, deleteUser: func(context.Context, storedUser, string) error { t.Fatal("missing account cleanup"); return nil }, throttle: time.Hour}
	if err := p.process(ctx, internalDeletionEvent{kind: "user.delete"}); err == nil {
		t.Fatal("canceled throttle acknowledged")
	}
	received := make(chan struct{}, 1)
	queue := feedbackTestQueue(t, func(w http.ResponseWriter, r *http.Request) {
		if r.ParseForm() != nil || r.Form.Get("Action") != "ReceiveMessage" || r.Form.Get("WaitTimeSeconds") != "20" || r.Form.Get("MaxNumberOfMessages") != "10" {
			t.Error("native long poll")
		}
		received <- struct{}{}
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	})
	listener := startInternalEvents(context.Background(), &internalEventQueue{sqs: queue}, p, nil)
	select {
	case <-received:
	case <-time.After(time.Second):
		t.Fatal("listener did not poll")
	}
	done := make(chan struct{})
	go func() { listener.close(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("listener did not cancel/join")
	}
}
