// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"errors"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func openCassandra(c CassandraConfig) (*gocql.Session, error) {
	if !c.Endpoint.valid() || c.Keyspace == "" {
		return nil, errors.New("invalid Cassandra configuration")
	}
	cluster := gocql.NewCluster(c.Endpoint.Host)
	cluster.Port = c.Endpoint.Port
	cluster.Keyspace = c.Keyspace
	cluster.Consistency = gocql.LocalQuorum
	cluster.ConnectTimeout = 10 * time.Second
	cluster.Timeout = 10 * time.Second
	if c.Datacentre != "" {
		cluster.HostFilter = gocql.DataCentreHostFilter(c.Datacentre)
	}
	if c.TLSCA != "" {
		tls, err := verifiedTLS(c.Endpoint.Host, c.TLSCA)
		if err != nil {
			return nil, err
		}
		cluster.SslOpts = &gocql.SslOptions{Config: tls, EnableHostVerification: true}
	}
	session, err := cluster.CreateSession()
	if err != nil {
		return nil, errors.New("Cassandra initialization failed")
	}
	return session, nil
}

type missedNotificationStore interface {
	MarkMissed(context.Context, gocql.UUID, string) error
}
type cassandraMissedStore struct{ session *gocql.Session }

func (s cassandraMissedStore) MarkMissed(ctx context.Context, user gocql.UUID, client string) error {
	err := s.session.Query("INSERT INTO missed_notifications (user_id, client_id) VALUES (?, ?)", user, client).
		WithContext(ctx).Consistency(gocql.LocalQuorum).
		RetryPolicy(&gocql.ExponentialBackoffRetryPolicy{NumRetries: 1, Min: 100 * time.Millisecond, Max: 1600 * time.Millisecond}).Exec()
	if err != nil {
		return errors.New("missed notification write failed")
	}
	return nil
}
