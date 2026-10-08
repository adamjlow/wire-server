package cannon

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
	amqp "github.com/rabbitmq/amqp091-go"
)

type Backends struct {
	Cassandra *gocql.Session
	Rabbit    *amqp.Connection
}

func Connect(c Config) (*Backends, error) {
	cluster := gocql.NewCluster(c.Cassandra.Endpoint.Host)
	cluster.Port = int(c.Cassandra.Endpoint.Port)
	cluster.Keyspace = c.Cassandra.Keyspace
	cluster.Consistency = gocql.LocalQuorum
	cluster.Timeout = 10 * time.Second
	cluster.ConnectTimeout = 10 * time.Second
	if c.Cassandra.Datacentre != "" {
		cluster.HostFilter = gocql.DataCentreHostFilter(c.Cassandra.Datacentre)
	}
	if c.Cassandra.TLSCA != "" {
		config, err := verifiedTLS(c.Cassandra.Endpoint.Host, c.Cassandra.TLSCA)
		if err != nil {
			return nil, err
		}
		cluster.SslOpts = &gocql.SslOptions{Config: config, EnableHostVerification: true}
	}
	session, err := cluster.CreateSession()
	if err != nil {
		return nil, fmt.Errorf("Cassandra initialization failed")
	}
	connection, err := dialRabbit(c, 0)
	if err != nil {
		session.Close()
		return nil, err
	}
	return &Backends{Cassandra: session, Rabbit: connection}, nil
}
func dialRabbit(c Config, id uint64) (*amqp.Connection, error) {
	user, hasUser := os.LookupEnv("RABBITMQ_USERNAME")
	password, hasPassword := os.LookupEnv("RABBITMQ_PASSWORD")
	if !hasUser || !hasPassword {
		return nil, fmt.Errorf("RabbitMQ credentials must be supplied in environment")
	}
	scheme := "amqp"
	if c.RabbitMQ.EnableTLS {
		scheme = "amqps"
	}
	uri := url.URL{Scheme: scheme, Host: c.RabbitMQ.Address(), User: url.UserPassword(user, password), Path: "/" + c.RabbitMQ.VHost}
	// AMQP URI vhost must occupy one encoded path component, including '/' when
	// configured as the default vhost. Never interpolate credentials into logs.
	uri.RawPath = "/" + url.PathEscape(c.RabbitMQ.VHost)
	config := amqp.Config{Properties: amqp.Table{"connection_name": "pool " + strconv.FormatUint(id, 10)}, Dial: func(network, address string) (net.Conn, error) {
		return net.DialTimeout(network, address, 10*time.Second)
	}}
	if c.RabbitMQ.EnableTLS {
		tlsConfig, err := verifiedTLS(c.RabbitMQ.Host, c.RabbitMQ.CACert)
		if err != nil {
			return nil, err
		}
		config.TLSClientConfig = tlsConfig
	}
	conn, err := amqp.DialConfig(uri.String(), config)
	if err != nil {
		return nil, fmt.Errorf("RabbitMQ initialization failed")
	}
	return conn, nil
}
func (b *Backends) Close() {
	if b.Rabbit != nil {
		_ = b.Rabbit.Close()
	}
	if b.Cassandra != nil {
		b.Cassandra.Close()
	}
}
func (b *Backends) missed(ctx context.Context, user, client string) (bool, error) {
	id, err := gocql.ParseUUID(user)
	if err != nil {
		return false, err
	}
	var result gocql.UUID
	err = b.Cassandra.Query("SELECT user_id FROM missed_notifications WHERE user_id = ? and client_id = ?", id, client).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.ExponentialBackoffRetryPolicy{NumRetries: 5, Min: 100 * time.Millisecond, Max: 1600 * time.Millisecond}).Scan(&result)
	if err == gocql.ErrNotFound {
		return false, nil
	}
	return err == nil, err
}

func parseUserUUID(user string) (gocql.UUID, error) { return gocql.ParseUUID(user) }
