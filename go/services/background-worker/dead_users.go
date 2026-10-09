// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
	amqp "github.com/rabbitmq/amqp091-go"
)

func parseUserID(value string) (gocql.UUID, error) {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return gocql.UUID{}, errors.New("invalid user identifier")
	}
	id, err := gocql.ParseUUID(value)
	if err != nil {
		return id, errors.New("invalid user identifier")
	}
	return id, nil
}

func parseDeathQueue(headers amqp.Table) (gocql.UUID, string, error) {
	queue, ok := headers["x-last-death-queue"].(string)
	if !ok {
		return gocql.UUID{}, "", errors.New("missing last death queue")
	}
	parts := strings.Split(queue, ".")
	if len(parts) != 3 || parts[0] != "user-notifications" {
		return gocql.UUID{}, "", errors.New("invalid last death queue")
	}
	user, err := parseUserID(parts[1])
	if err != nil {
		return user, "", err
	}
	if parts[2] == "" {
		return user, "", errors.New("invalid client identifier")
	}
	for _, c := range parts[2] {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return user, "", errors.New("invalid client identifier")
		}
	}
	// Source ClientId permits any hex numeral fitting uint64, including long
	// strings of leading zeros; Cassandra stores its canonical lower-case value.
	text := strings.TrimLeft(parts[2], "0")
	if text == "" {
		text = "0"
	}
	client, err := strconv.ParseUint(text, 16, 64)
	if err != nil {
		return user, "", errors.New("invalid client identifier")
	}
	return user, strconv.FormatUint(client, 16), nil
}

func handleDeadUser(ctx context.Context, store missedNotificationStore, d amqp.Delivery) error {
	if d.DeliveryMode == amqp.Transient {
		return d.Ack(false)
	}
	user, client, err := parseDeathQueue(d.Headers)
	if err != nil {
		return err
	}
	if err = store.MarkMissed(ctx, user, client); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return d.Ack(false)
}

func (s *Service) runDeadWatcher(ctx context.Context) {
	// This store is also shared by the remaining worker concerns once startup
	// is consolidated; the watcher owns it during the current construction slice.
	var session *gocql.Session
	defer func() {
		s.mark(1, false)
		if session != nil {
			session.Close()
		}
	}()
	for ctx.Err() == nil {
		if session == nil {
			var err error
			session, err = openCassandra(s.Config.Cassandra)
			if err != nil {
				if !wait(ctx, time.Second) {
					return
				}
				continue
			}
		}
		conn, err := s.openRabbit(ctx, "dead-user-notifications-watcher")
		if err == nil {
			channel, e := conn.Channel()
			err = e
			if err == nil {
				_ = s.deadWatcherSession(ctx, channel, cassandraMissedStore{session})
				_ = channel.Close()
			}
			_ = conn.Close()
		}
		s.mark(1, false)
		if !wait(ctx, time.Second) {
			return
		}
	}
}

func (s *Service) deadWatcherSession(ctx context.Context, channel *amqp.Channel, store missedNotificationStore) error {
	const name = "dead-user-notifications"
	if _, err := channel.QueueDeclare(name, true, false, false, false, amqp.Table{"x-queue-type": "quorum"}); err != nil {
		return err
	}
	deliveries, err := channel.Consume(name, "dead-user-watcher", false, false, false, false, nil)
	if err != nil {
		return err
	}
	s.mark(1, true)
	defer s.mark(1, false)
	defer channel.Cancel("dead-user-watcher", false)
	for {
		select {
		case <-ctx.Done():
			return nil
		case d, ok := <-deliveries:
			if !ok {
				return errors.New("dead user consumer closed")
			}
			if err = handleDeadUser(ctx, store, d); err != nil {
				return err
			}
		}
	}
}
