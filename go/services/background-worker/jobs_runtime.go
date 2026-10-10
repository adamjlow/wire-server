// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"net/http"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func (s *Service) runJobs(ctx context.Context) {
	var galley, brig *gocql.Session
	var postgres *postgresStore
	var bots *externalBotDelivery
	transport, err := newInternalHTTPTransport()
	if err != nil {
		return
	}
	defer transport.CloseIdleConnections()
	defer func() {
		s.mark(2, false)
		if bots != nil {
			bots.wg.Wait()
		}
		if postgres != nil {
			postgres.Pool.Close()
		}
		if brig != nil {
			brig.Close()
		}
		if galley != nil {
			galley.Close()
		}
	}()
	for ctx.Err() == nil {
		if galley == nil {
			var err error
			galley, err = openCassandra(s.Galley.Cassandra)
			if err != nil {
				if !wait(ctx, time.Second) {
					return
				}
				continue
			}
		}
		if brig == nil {
			var err error
			brig, err = openCassandra(s.Config.CassandraBrig)
			if err != nil {
				if !wait(ctx, time.Second) {
					return
				}
				continue
			}
		}
		if bots == nil {
			bots = newExternalBotDelivery(ctx, cassandraBotStore{Session: brig})
		}
		if postgres == nil {
			var err error
			postgres, err = openPostgres(ctx, s.Config.PostgresPool, s.Galley)
			if err != nil {
				if !wait(ctx, time.Second) {
					return
				}
				continue
			}
			postgres.CassandraCleanup = migratedConversationCleanup{Cassandra: cassandraConversationStore{Session: galley}, Postgres: postgres}
		}
		conn, err := s.openRabbit(ctx, "background-job-consumer")
		if err == nil {
			channel, err := conn.Channel()
			if err == nil {
				publisherChannel, err := conn.Channel()
				if err == nil {
					publisher := rabbitJobPublisher{Channel: publisherChannel}
					notifier := &conversationJoinNotifier{Queue: publisher, Bots: bots, Client: &http.Client{Transport: transport}, Gundeck: s.Config.Gundeck, Domain: s.Galley.Settings.Domain}
					store := &routedGroupStore{Groups: postgres, Postgres: postgres, Cassandra: cassandraConversationStore{Session: galley}, Location: s.Config.Storage.Conversation}
					dispatcher := &groupSyncDispatcher{Store: store, Notifier: notifier, Publisher: publisher, Domain: s.Galley.Settings.Domain}
					_ = s.jobSession(ctx, channel, dispatcher)
					// Bot callbacks belong to the process and survive a Rabbit
					// session reconnect; they are drained before Brig closes.
					_ = publisherChannel.Close()
				}
				_ = channel.Close()
			}
			_ = conn.Close()
		}
		s.mark(2, false)
		if !wait(ctx, time.Second) {
			return
		}
	}
}
