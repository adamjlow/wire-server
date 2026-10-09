// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func (s *Service) runMigrations(ctx context.Context) {
	if !s.Config.MigrateBlockList && !s.Config.MigratePasswordReset && !s.Config.MigrateCodes && !s.Config.MigrateDomains && !s.Config.MigrateTeamFeatures && !s.Config.MigrateUsers {
		return
	}
	var session, galley *gocql.Session
	var postgres *postgresStore
	defer func() {
		if postgres != nil {
			postgres.Pool.Close()
		}
		if session != nil {
			session.Close()
		}
		if galley != nil {
			galley.Close()
		}
	}()
	for ctx.Err() == nil {
		if session == nil && (s.Config.MigrateBlockList || s.Config.MigratePasswordReset || s.Config.MigrateDomains || s.Config.MigrateUsers) {
			var err error
			session, err = openCassandra(s.Config.CassandraBrig)
			if err != nil {
				if !wait(ctx, time.Second) {
					return
				}
				continue
			}
		}
		if galley == nil && (s.Config.MigrateCodes || s.Config.MigrateTeamFeatures) {
			var err error
			galley, err = openCassandra(s.Galley.Cassandra)
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
	start := func(name string, cassandra *gocql.Session, migrate func(context.Context, *gocql.Session, *postgresStore) error) {
		s.migrationMetric(name, func(*migrationMetrics) {})
		wg.Go(func() {
			if err := migrate(ctx, cassandra, postgres); err != nil && ctx.Err() == nil {
				// Native leaves the failed migration stopped until restart. Do
				// not log row keys, reset codes or raw database diagnostics.
				slog.Error("migration failed; restart required", "migration", name)
			}
		})
	}
	if s.Config.MigrateBlockList {
		start("block_list", session, s.migrateBlockList)
	}
	if s.Config.MigratePasswordReset {
		start("password_reset", session, s.migratePasswordReset)
	}
	if s.Config.MigrateCodes {
		start("conv_codes", galley, s.migrateCodes)
	}
	if s.Config.MigrateDomains {
		start("domain_registration", session, s.migrateDomains)
	}
	if s.Config.MigrateTeamFeatures {
		start("team_features", galley, s.migrateTeamFeatures)
	}
	if s.Config.MigrateUsers {
		start("users", session, s.migrateUsers)
	}
	wg.Wait()
}
