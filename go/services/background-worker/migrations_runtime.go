// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func (s *Service) runIdentityMigrations(ctx context.Context) {
	if !s.Config.MigrateBlockList && !s.Config.MigratePasswordReset {
		return
	}
	var session *gocql.Session
	var postgres *postgresStore
	defer func() {
		if postgres != nil {
			postgres.Pool.Close()
		}
		if session != nil {
			session.Close()
		}
	}()
	for ctx.Err() == nil {
		if session == nil {
			var err error
			session, err = openCassandra(s.Config.CassandraBrig)
			if err != nil {
				if !wait(ctx, time.Second) {
					return
				}
				continue
			}
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
		}
		break
	}
	if ctx.Err() != nil {
		return
	}
	var wg sync.WaitGroup
	start := func(name string, migrate func(context.Context, *gocql.Session, *postgresStore) error) {
		s.migrationMetric(name, func(*migrationMetrics) {})
		wg.Go(func() {
			if err := migrate(ctx, session, postgres); err != nil && ctx.Err() == nil {
				// Native leaves the failed migration stopped until restart. Do
				// not log row keys, reset codes or raw database diagnostics.
				slog.Error("migration failed; restart required", "migration", name)
			}
		})
	}
	if s.Config.MigrateBlockList {
		start("block_list", s.migrateBlockList)
	}
	if s.Config.MigratePasswordReset {
		start("password_reset", s.migratePasswordReset)
	}
	wg.Wait()
}
