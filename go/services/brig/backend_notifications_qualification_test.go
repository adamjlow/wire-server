//go:build brig_storage_qualification

// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
	amqp "github.com/rabbitmq/amqp091-go"
)

func TestOwnedBackendNotifications(t *testing.T) {
	host := os.Getenv("BRIG_QUALIFICATION_RABBIT_HOST")
	if host == "" {
		t.Fatal("owned broker configuration required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	c := Config{Federator: &Endpoint{Host: "unused", Port: 1}, Rabbit: &brigRabbitConfig{Endpoint: Endpoint{Host: host, Port: 5672}, VHost: "/"}}
	q, err := newBrigNotificationQueue(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	defer q.close()
	user, target := gocql.TimeUUID(), gocql.TimeUUID()
	body, err := deletionNotificationBundle("example.com", user, []storedConnection{{to: target}}, "synthetic-queue-proof")
	if err != nil {
		t.Fatal(err)
	}
	reader, err := q.open()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	channel, err := reader.Channel()
	if err != nil {
		t.Fatal(err)
	}
	defer channel.Close()
	var deliveries <-chan amqp.Delivery
	for i := 0; i < 2; i++ {
		if err := q.enqueue(ctx, "remote.example.com", body); err != nil {
			t.Fatal("persistent enqueue", err)
		}
		if i == 0 {
			deliveries, err = channel.Consume("backend-notifications.remote.example.com", "owned-proof", false, false, false, false, nil)
			if err != nil {
				t.Fatal(err)
			}
		}
		select {
		case message, ok := <-deliveries:
			if !ok {
				t.Fatal("owned consumer closed before delivery")
			}
			if message.DeliveryMode != amqp.Persistent || message.ContentType != "application/json" || !bytes.Equal(message.Body, body) {
				t.Fatal("native publication properties/body changed")
			}
			if err := message.Ack(false); err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal("published message unavailable")
		}
		if i == 0 {
			q.mu.Lock()
			old := q.session
			q.mu.Unlock()
			if old == nil {
				t.Fatal("publisher channel missing")
			}
			if err := old.connection.Close(); err != nil {
				t.Fatal(err)
			}
			for {
				q.mu.Lock()
				current := q.session
				q.mu.Unlock()
				if current != nil && current != old {
					break
				}
				if waitBrigRetry(ctx, 10*time.Millisecond) != nil {
					t.Fatal("publisher failed to reconnect")
				}
			}
		}
	}
}
