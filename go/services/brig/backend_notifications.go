// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

type brigRabbitConfig struct {
	Endpoint `yaml:",inline"`
	VHost    string `yaml:"vHost"`
	TLS      bool   `yaml:"enableTls"`
	CA       string `yaml:"caCert"`
	Insecure bool   `yaml:"insecureSkipVerifyTls"`
}

type brigRabbitSession struct {
	connection *amqp.Connection
	channel    *amqp.Channel
}
type brigNotificationQueue struct {
	config             brigRabbitConfig
	username, password string
	tls                *tls.Config
	ctx                context.Context
	cancel             context.CancelFunc
	workers            sync.WaitGroup
	mu                 sync.Mutex
	session            *brigRabbitSession
	ready              chan struct{}
}

func (*brigNotificationQueue) String() string { return "<BrigNotificationQueue>" }

func newBrigNotificationQueue(ctx context.Context, c Config) (*brigNotificationQueue, error) {
	if c.Federator == nil {
		return nil, nil
	}
	if c.Rabbit == nil || !c.Rabbit.Endpoint.valid() || c.Rabbit.TLS && c.Rabbit.Insecure {
		return nil, errors.New("invalid RabbitMQ configuration")
	}
	username, ok := os.LookupEnv("RABBITMQ_USERNAME")
	password, present := os.LookupEnv("RABBITMQ_PASSWORD")
	if !ok || !present {
		return nil, errors.New("RabbitMQ credentials unavailable")
	}
	q := &brigNotificationQueue{config: *c.Rabbit, username: username, password: password, ready: make(chan struct{})}
	if q.config.TLS {
		q.tls = &tls.Config{ServerName: q.config.Host, MinVersion: tls.VersionTLS12}
		if q.config.CA != "" {
			data, err := os.ReadFile(q.config.CA)
			if err != nil {
				return nil, errors.New("RabbitMQ trusted CA unavailable")
			}
			q.tls.RootCAs = x509.NewCertPool()
			if !q.tls.RootCAs.AppendCertsFromPEM(data) {
				return nil, errors.New("invalid RabbitMQ trusted CA")
			}
		}
	}
	q.ctx, q.cancel = context.WithCancel(ctx)
	q.workers.Add(1)
	go func() { defer q.workers.Done(); q.run() }()
	return q, nil
}

func (q *brigNotificationQueue) close() {
	if q != nil {
		q.cancel()
		q.workers.Wait()
	}
}

func (q *brigNotificationQueue) open() (*amqp.Connection, error) {
	scheme := "amqp"
	if q.config.TLS {
		scheme = "amqps"
	}
	uri := url.URL{Scheme: scheme, Host: q.config.Address(), User: url.UserPassword(q.username, q.password), Path: "/" + q.config.VHost, RawPath: "/" + url.PathEscape(q.config.VHost)}
	stopHandshake := func() bool { return false }
	defer func() { stopHandshake() }()
	connection, err := amqp.DialConfig(uri.String(), amqp.Config{TLSClientConfig: q.tls, Properties: amqp.Table{"connection_name": "brig-backend-notifications"}, Dial: func(network, address string) (net.Conn, error) {
		conn, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(q.ctx, network, address)
		if err != nil {
			return nil, err
		}
		if err = conn.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
			conn.Close()
			return nil, err
		}
		stopHandshake = context.AfterFunc(q.ctx, func() { conn.Close() })
		return conn, nil
	}})
	if err != nil {
		return nil, errors.New("RabbitMQ connection failed")
	}
	return connection, nil
}

func (q *brigNotificationQueue) run() {
	for q.ctx.Err() == nil {
		conn, err := q.open()
		if err == nil {
			stop := context.AfterFunc(q.ctx, func() { conn.CloseDeadline(time.Now()) })
			channel, err := conn.Channel()
			if err == nil {
				closed := channel.NotifyClose(make(chan *amqp.Error, 1))
				q.mu.Lock()
				q.session = &brigRabbitSession{connection: conn, channel: channel}
				close(q.ready)
				q.mu.Unlock()
				select {
				case <-q.ctx.Done():
				case <-closed:
				}
				q.mu.Lock()
				q.session = nil
				q.ready = make(chan struct{})
				q.mu.Unlock()
			}
			conn.CloseDeadline(time.Now().Add(time.Second))
			stop()
		}
		if waitBrigRetry(q.ctx, time.Second) != nil {
			return
		}
	}
}

func (q *brigNotificationQueue) enqueueOnce(ctx context.Context, domain string, body []byte) error {
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for {
		q.mu.Lock()
		session, ready := q.session, q.ready
		q.mu.Unlock()
		if session != nil {
			// AMQP's synchronous methods lack context cancellation. Closing the
			// connection interrupts a blocked declaration/publication; run reconnects.
			stop := context.AfterFunc(ctx, func() { session.connection.CloseDeadline(time.Now()) })
			defer stop()
			name := "backend-notifications." + strings.ToLower(domain)
			if _, err := session.channel.QueueDeclare(name, true, false, false, false, amqp.Table{"x-queue-type": "quorum", "x-single-active-consumer": true}); err != nil {
				return errors.New("backend notification queue declaration failed")
			}
			if err := session.channel.PublishWithContext(ctx, "", name, false, false, amqp.Publishing{ContentType: "application/json", DeliveryMode: amqp.Persistent, Body: body}); err != nil {
				return errors.New("backend notification publication failed")
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-q.ctx.Done():
			return errors.New("backend notification publisher stopped")
		case <-timer.C:
			return errors.New("backend notification channel unavailable")
		case <-ready:
		}
	}
}

func (q *brigNotificationQueue) enqueue(ctx context.Context, domain string, body []byte) error {
	if q == nil {
		return errors.New("federation not configured")
	}
	if !deletionEventDomain(domain) {
		return errors.New("invalid backend notification domain")
	}
	for attempt := 0; ; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err := q.enqueueOnce(ctx, domain, body)
		if err == nil {
			return nil
		}
		if attempt >= 3 || q.ctx.Err() != nil {
			return err
		}
		if waitBrigRetry(ctx, time.Second) != nil {
			return ctx.Err()
		}
	}
}
