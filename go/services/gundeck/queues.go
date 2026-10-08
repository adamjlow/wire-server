package gundeck

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const notificationExchange = "user-notifications"
const notificationDead = "dead-user-notifications"

func (s *Server) rabbitChannel() (*amqp.Channel, error) {
	// Caller serializes channel access, including replacement after broker loss.
	if s.rabbitChan != nil && !s.rabbitChan.IsClosed() {
		return s.rabbitChan, nil
	}
	if s.rabbit != nil {
		_ = s.rabbit.Close()
	}
	user, userOK := os.LookupEnv("RABBITMQ_USERNAME")
	password, passwordOK := os.LookupEnv("RABBITMQ_PASSWORD")
	if !userOK || !passwordOK {
		return nil, fmt.Errorf("RabbitMQ credentials must be supplied in environment")
	}
	c := s.Config.RabbitMQ
	scheme := "amqp"
	if c.EnableTLS {
		scheme = "amqps"
	}
	uri := url.URL{Scheme: scheme, Host: c.Address(), User: url.UserPassword(user, password), Path: "/" + c.VHost, RawPath: "/" + url.PathEscape(c.VHost)}
	config := amqp.Config{Properties: amqp.Table{"connection_name": "gundeck"}, Dial: func(network, address string) (net.Conn, error) {
		return net.DialTimeout(network, address, 10*time.Second)
	}}
	if c.EnableTLS {
		var err error
		config.TLSClientConfig, err = verifiedTLS(c.Host, c.CA)
		if err != nil {
			return nil, err
		}
	}
	conn, err := amqp.DialConfig(uri.String(), config)
	if err != nil {
		return nil, fmt.Errorf("RabbitMQ initialization failed")
	}
	channel, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("RabbitMQ channel initialization failed")
	}
	for _, name := range []string{notificationExchange, notificationDead} {
		if err = channel.ExchangeDeclare(name, "direct", true, false, false, false, nil); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("RabbitMQ exchange initialization failed")
		}
	}
	if _, err = channel.QueueDeclare(notificationDead, true, false, false, false, amqp.Table{"x-queue-type": "quorum"}); err == nil {
		err = channel.QueueBind(notificationDead, notificationDead, notificationDead, false, nil)
	}
	if err == nil && s.Config.Settings.CellsQueue != nil {
		_, err = channel.QueueDeclare(*s.Config.Settings.CellsQueue, true, false, false, false, amqp.Table{"x-queue-type": "quorum"})
	}
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("RabbitMQ queue initialization failed")
	}
	s.rabbit = conn
	s.rabbitChan = channel
	return channel, nil
}
func (s *Server) registerConsumable(ctx context.Context, user, client string) error {
	if !s.Config.Settings.Consumable {
		return nil
	}
	s.rabbitMu.Lock()
	defer s.rabbitMu.Unlock()
	channel, err := s.rabbitChannel()
	if err != nil {
		return err
	}
	name := notificationExchange + "." + user + "." + client
	_, err = channel.QueueDeclare(name, true, false, false, false, amqp.Table{"x-queue-type": "quorum", "x-dead-letter-exchange": notificationDead, "x-dead-letter-routing-key": notificationDead})
	if err != nil {
		return err
	}
	for _, key := range []string{user, user + "." + client} {
		if err = channel.QueueBind(name, key, notificationExchange, false, nil); err != nil {
			return err
		}
	}
	return nil
}
func (s *Server) publishQueue(ctx context.Context, exchange, key string, payload []byte, transient bool) error {
	s.rabbitMu.Lock()
	defer s.rabbitMu.Unlock()
	channel, err := s.rabbitChannel()
	if err != nil {
		return err
	}
	mode := uint8(amqp.Persistent)
	expiration := strconv.FormatUint(uint64(s.Config.Settings.TTL)*1000, 10)
	if transient {
		mode = amqp.Transient
		expiration = "0"
	}
	return channel.PublishWithContext(ctx, exchange, key, false, false, amqp.Publishing{ContentType: "application/json", DeliveryMode: mode, Expiration: expiration, Body: payload})
}
