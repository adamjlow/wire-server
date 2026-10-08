package cannon

import (
	"errors"
	"sync"

	amqp "github.com/rabbitmq/amqp091-go"
)

var errCapacity = errors.New("RabbitMQ channel capacity exhausted")

type pooledConnection struct {
	connection *amqp.Connection
	channels   map[*amqp.Channel]struct{}
}
type rabbitPool struct {
	mu          sync.Mutex
	config      Config
	connections []*pooledConnection
	nextID      uint64
	draining    bool
}

func newPool(config Config, initial *amqp.Connection) *rabbitPool {
	return &rabbitPool{config: config, connections: []*pooledConnection{{initial, make(map[*amqp.Channel]struct{})}}, nextID: 1}
}
func (p *rabbitPool) acquire() (*amqp.Channel, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.draining {
		return nil, errCapacity
	}
	active := p.connections[:0]
	for _, conn := range p.connections {
		if !conn.connection.IsClosed() {
			active = append(active, conn)
		}
	}
	p.connections = active
	var chosen *pooledConnection
	for _, conn := range active {
		if chosen == nil || len(conn.channels) < len(chosen.channels) {
			chosen = conn
		}
	}
	if chosen == nil || len(chosen.channels) >= p.config.MaxChannels {
		if len(active) >= p.config.MaxConnections {
			return nil, errCapacity
		}
		connection, err := dialRabbit(p.config, p.nextID)
		if err != nil {
			return nil, err
		}
		p.nextID++
		chosen = &pooledConnection{connection, make(map[*amqp.Channel]struct{})}
		p.connections = append(p.connections, chosen)
	}
	channel, err := chosen.connection.Channel()
	if err != nil {
		return nil, err
	}
	chosen.channels[channel] = struct{}{}
	if err = channel.Qos(500, 0, false); err != nil {
		delete(chosen.channels, channel)
		channel.Close()
		return nil, err
	}
	return channel, nil
}
func (p *rabbitPool) release(channel *amqp.Channel) {
	_ = channel.Close()
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, conn := range p.connections {
		delete(conn.channels, channel)
	}
}
func (p *rabbitPool) channelsForDrain() []*amqp.Channel {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.draining = true
	var channels []*amqp.Channel
	for _, conn := range p.connections {
		for channel := range conn.channels {
			channels = append(channels, channel)
		}
	}
	return channels
}
func (p *rabbitPool) close() {
	p.mu.Lock()
	connections := append([]*pooledConnection(nil), p.connections...)
	p.mu.Unlock()
	for _, conn := range connections {
		_ = conn.connection.Close()
	}
}
