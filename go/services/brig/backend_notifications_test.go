// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestBackendNotificationsAdmissionAndCancellation(t *testing.T) {
	var disabled *brigNotificationQueue
	if disabled.enqueue(context.Background(), "example.com", nil) == nil {
		t.Fatal("unconfigured federation returned success")
	}
	c := Config{Federator: &Endpoint{Host: "localhost", Port: 1}, Rabbit: &brigRabbitConfig{Endpoint: Endpoint{Host: "localhost", Port: 1}, VHost: "/"}}
	t.Setenv("RABBITMQ_USERNAME", "synthetic-user")
	t.Setenv("RABBITMQ_PASSWORD", "synthetic-password")
	for _, mode := range []string{"bypass", "invalid-ca", "missing-credentials"} {
		copy := c
		config := *c.Rabbit
		copy.Rabbit = &config
		switch mode {
		case "bypass":
			config.TLS = true
			config.Insecure = true
		case "invalid-ca":
			config.TLS = true
			config.CA = t.TempDir() + "/missing-ca"
		case "missing-credentials":
			os.Unsetenv("RABBITMQ_PASSWORD")
		}
		q, err := newBrigNotificationQueue(context.Background(), copy)
		if err == nil {
			q.close()
			t.Fatal("unsafe or incomplete queue configuration accepted", mode)
		}
		if strings.Contains(err.Error(), "synthetic-password") {
			t.Fatal("credential leaked")
		}
	}
	t.Setenv("RABBITMQ_PASSWORD", "synthetic-password")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	q, err := newBrigNotificationQueue(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	defer q.close()
	if q.enqueue(ctx, "example.com", nil) == nil {
		t.Fatal("canceled enqueue succeeded")
	}
	if q.enqueue(context.Background(), "../../other", nil) == nil {
		t.Fatal("unsafe target accepted")
	}
	done := make(chan struct{})
	go func() { q.close(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("publisher shutdown blocked")
	}
}
