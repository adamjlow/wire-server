// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"net/http"

	"github.com/apache/cassandra-gocql-driver/v2"
)

// Native startWorker applies the Arbiter schema before exposing the health
// listener, then starts both pools. Failed setup cannot advertise a running worker.
func (s *Service) prepareScheduled(ctx context.Context) (*scheduledRuntime, func(), error) {
	var galley, brig *gocql.Session
	var pg *postgresStore
	var bots *externalBotDelivery
	queue := &scheduledNotificationQueue{Service: s, ready: make(chan struct{})}
	botCtx, stopBots := context.WithCancel(context.WithoutCancel(ctx))
	ordinaryTransport := &http.Transport{Proxy: nil}
	ordinary := &http.Client{Transport: ordinaryTransport}
	federation := notificationClient()
	cleanup := func() {
		stopBots()
		if bots != nil {
			bots.wg.Wait()
		}
		ordinaryTransport.CloseIdleConnections()
		federation.CloseIdleConnections()
		if pg != nil {
			pg.Pool.Close()
		}
		if brig != nil {
			brig.Close()
		}
		if galley != nil {
			galley.Close()
		}
	}
	fail := func(err error) (*scheduledRuntime, func(), error) { cleanup(); return nil, nil, err }
	if err := runScheduledSchema(ctx, s.Config.PostgresPool, s.Galley); err != nil {
		return fail(err)
	}
	var err error
	galley, err = openCassandra(s.Galley.Cassandra)
	if err != nil {
		return fail(err)
	}
	brig, err = openCassandra(s.Config.CassandraBrig)
	if err != nil {
		return fail(err)
	}
	pg, err = openPostgres(ctx, s.Config.PostgresPool, s.Galley)
	if err != nil {
		return fail(err)
	}
	cass := cassandraConversationStore{Session: galley}
	pg.CassandraCleanup = migratedConversationCleanup{Cassandra: cass, Postgres: pg}
	bots = newExternalBotDelivery(botCtx, cassandraBotStore{Session: brig})
	pools, err := s.scheduledPools(pg, cass, bots, queue, ordinary, federation)
	if err != nil {
		return fail(err)
	}
	return &scheduledRuntime{Service: s, Postgres: pg, Pools: pools, Queue: queue}, cleanup, nil
}
