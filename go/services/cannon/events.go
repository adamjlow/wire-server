package cannon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/gorilla/websocket"
	amqp "github.com/rabbitmq/amqp091-go"
)

type clientAck struct {
	FullSync bool
	Tag      uint64
	Multiple bool
}

func parseAck(raw []byte) (clientAck, error) {
	var frame struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(raw, &frame) != nil {
		return clientAck{}, errors.New("failed-to-parse")
	}
	switch frame.Type {
	case "ack_full_sync":
		return clientAck{FullSync: true}, nil
	case "ack":
		var data struct {
			Tag      json.RawMessage `json:"delivery_tag"`
			Multiple *bool           `json:"multiple"`
		}
		if json.Unmarshal(frame.Data, &data) != nil || len(data.Tag) == 0 || data.Multiple == nil {
			return clientAck{}, errors.New("failed-to-parse")
		}
		tag, err := decodeWord64(data.Tag)
		if err != nil {
			return clientAck{}, errors.New("failed-to-parse")
		}
		return clientAck{Tag: tag, Multiple: *data.Multiple}, nil
	default:
		return clientAck{}, errors.New("failed-to-parse")
	}
}
func (s *Server) AttachBackends(backends *Backends) {
	s.backends = backends
	s.pool = newPool(s.Config, backends.Rabbit)
}
func (s *Server) events(w http.ResponseWriter, r *http.Request, user string, clientID, marker *string) {
	if s.pool == nil {
		failure(w, r, 500, "server-error", "Server Error")
		return
	}
	channel, err := s.pool.acquire()
	if err != nil {
		if errors.Is(err, errCapacity) {
			plain(w, r, 503, "")
		} else {
			failure(w, r, 500, "server-error", "Server Error")
		}
		return
	}
	defer s.pool.release(channel)
	var queue amqp.Queue
	if clientID == nil {
		queue, err = channel.QueueDeclare("", false, true, true, false, nil)
		if err == nil {
			for _, key := range []string{user, user + ".temporary"} {
				if err = channel.QueueBind(queue.Name, key, "user-notifications", false, nil); err != nil {
					break
				}
			}
		}
	} else {
		queue, err = channel.QueueDeclare("user-notifications."+user+"."+*clientID, true, false, false, false, amqp.Table{"x-queue-type": "quorum", "x-dead-letter-exchange": "dead-user-notifications", "x-dead-letter-routing-key": "dead-user-notifications"})
	}
	if err != nil {
		failure(w, r, 500, "server-error", "Server Error")
		return
	}
	closed := channel.NotifyClose(make(chan *amqp.Error, 1))
	// Haskell AMQP consumeMsgs sends basic.consume with nowait=true. Preserve
	// that subscription sequence: waiting for consume-ok adds a different
	// scheduling boundary before initial queue delivery and WebSocket acceptance.
	deliveries, err := channel.Consume(queue.Name, "", false, false, false, true, nil)
	if err != nil {
		failure(w, r, 500, "server-error", "Server Error")
		return
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c := &liveSocket{conn: conn, done: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer conn.Close()
	defer close(c.done)
	activity, heartbeat := make(chan struct{}, 1), make(chan struct{}, 1)
	conn.SetPingHandler(func(data string) error {
		select {
		case activity <- struct{}{}:
		default:
		}
		return c.write(websocket.PongMessage, []byte(data))
	})
	conn.SetPongHandler(func(string) error {
		select {
		case heartbeat <- struct{}{}:
		default:
		}
		return nil
	})
	go func() { defer conn.Close(); s.eventHeartbeat(ctx, c, activity, heartbeat); cancel() }()
	go func() {
		select {
		case <-ctx.Done():
		case <-closed:
			cancel()
			c.close(1001, "")
			conn.Close()
		}
	}()
	if clientID != nil {
		missed, err := s.backends.missed(ctx, user, *clientID)
		if err != nil {
			c.close(1003, "internal-error")
			return
		}
		if missed {
			if err = c.write(websocket.BinaryMessage, []byte(`{"type":"notifications_missed"}`)); err != nil {
				return
			}
			_, raw, err := conn.ReadMessage()
			if err != nil {
				return
			}
			ack, err := parseAck(raw)
			if err != nil {
				c.close(1003, "failed-to-parse")
				return
			}
			if !ack.FullSync {
				c.close(1003, "unexpected-ack")
				return
			}
			if err = s.backends.clearMissed(ctx, user, *clientID); err != nil {
				c.close(1003, "internal-error")
				return
			}
		}
		if marker != nil {
			err = channel.PublishWithContext(ctx, "", queue.Name, false, false, amqp.Publishing{Body: []byte(*marker), ContentType: "text/plain; charset=utf-8", DeliveryMode: amqp.Persistent, Expiration: strconv.FormatInt(s.Config.NotificationTTL, 10), Type: "synchronization"})
			if err != nil {
				c.close(1003, "internal-error")
				return
			}
		}
	}
	sending := make(chan error, 1)
	go func() {
		defer conn.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case delivery, ok := <-deliveries:
				if !ok {
					c.close(1001, "")
					return
				}
				var frame any
				if delivery.Type == "synchronization" {
					frame = map[string]any{"type": "synchronization", "data": map[string]any{"marker_id": string(delivery.Body), "delivery_tag": delivery.DeliveryTag}}
				} else {
					var event struct {
						ID      string           `json:"id"`
						Payload []map[string]any `json:"payload"`
					}
					valid := decodeNumbers(delivery.Body, &event) == nil && isUUID(event.ID) && len(event.Payload) > 0
					for _, value := range event.Payload {
						if value == nil {
							valid = false
						}
					}
					if !valid {
						_ = delivery.Reject(false)
						continue
					}
					event.ID = strings.ToLower(event.ID)
					frame = map[string]any{"type": "event", "data": map[string]any{"event": event, "delivery_tag": delivery.DeliveryTag}}
				}
				raw, err := json.Marshal(frame)
				if err == nil {
					err = c.write(websocket.BinaryMessage, raw)
				}
				if err != nil {
					sending <- err
					return
				}
			}
		}
	}()
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			select {
			case <-sending:
				c.close(1003, "internal-error")
			default:
			}
			return
		}
		ack, err := parseAck(raw)
		if err != nil {
			c.close(1003, "failed-to-parse")
			return
		}
		if ack.FullSync {
			c.close(1003, "unexpected-ack")
			return
		}
		if err = channel.Ack(ack.Tag, ack.Multiple); err != nil {
			c.close(1001, "")
			return
		}
	}
}
func (s *Server) eventHeartbeat(ctx context.Context, c *liveSocket, activity, heartbeat <-chan struct{}) {
	for {
		timer := time.NewTimer(time.Duration(s.Config.WS.Activity) * time.Microsecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-activity:
			timer.Stop()
			continue
		case <-timer.C:
		}
		if c.write(websocket.PingMessage, []byte("ping")) != nil {
			return
		}
		timer = time.NewTimer(time.Duration(s.Config.WS.Pong) * time.Microsecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-heartbeat:
			timer.Stop()
			continue
		case <-timer.C:
			c.close(1002, "inactivity")
			c.conn.Close()
			return
		}
	}
}
func (b *Backends) clearMissed(ctx context.Context, user, client string) error {
	id, err := parseUserUUID(user)
	if err != nil {
		return err
	}
	return b.Cassandra.Query("DELETE FROM missed_notifications WHERE user_id = ? and client_id = ?", id, client).WithContext(ctx).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).Exec()
}
