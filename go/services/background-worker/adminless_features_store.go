// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/jackc/pgx/v5"
)

func (g GalleyConfig) adminlessDefaults() (adminlessDefaults, error) {
	raw, err := json.Marshal(g.Settings.FeatureFlags[adminlessFeatureName])
	if err != nil {
		return adminlessDefaults{}, invalidAdminlessFeature()
	}
	return decodeAdminlessDefaults(raw)
}

type adminlessFeatureStore struct {
	Cassandra cassandraConversationStore
	Postgres  *postgresStore
	Location  storageLocation
	Defaults  adminlessDefaults
}

func (s adminlessFeatureStore) cassandraPatch(ctx context.Context, team string) (*featureMigrationRow, error) {
	tid, err := parseUserID(team)
	if err != nil {
		return nil, invalidAdminlessFeature()
	}
	if s.Cassandra.Session == nil {
		return nil, errors.New("adminless feature Cassandra unavailable")
	}
	values := map[string]any{"status": nil, "lock_status": nil, "config": nil}
	err = s.Cassandra.query(ctx, `SELECT status,lock_status,config FROM team_features_dyn WHERE team=? AND feature=?`, tid, adminlessFeatureName).MapScan(values)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.New("adminless feature Cassandra read failed")
	}
	values["team"], values["feature"] = tid, adminlessFeatureName
	row, err := decodeFeatureMigration(values)
	if err != nil {
		return nil, err
	}
	return &row, nil
}
func (s adminlessFeatureStore) postgresPatch(ctx context.Context, team string) (result *featureMigrationRow, err error) {
	tid, err := postgresUUID(team)
	if err != nil {
		return nil, invalidAdminlessFeature()
	}
	if s.Postgres == nil {
		return nil, errors.New("adminless feature Postgres unavailable")
	}
	err = retryPostgres(ctx, s.Postgres.Pool.Reset, func() error {
		result = nil
		conn, err := s.Postgres.acquire(ctx)
		if err != nil {
			return err
		}
		defer conn.Release()
		row := featureMigrationRow{Team: tid, Name: adminlessFeatureName}
		err = conn.QueryRow(ctx, `SELECT status,lock_status,config FROM team_features WHERE team=$1::uuid AND feature=$2::text`, tid, adminlessFeatureName).Scan(&row.Status, &row.LockStatus, &row.Config)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return postgresFailure(err, "adminless feature Postgres read failed")
		}
		for _, field := range []*int32{row.Status, row.LockStatus} {
			if field != nil && *field != 0 && *field != 1 {
				return invalidAdminlessFeature()
			}
		}
		result = &row
		return nil
	})
	return
}
func (s adminlessFeatureStore) Feature(ctx context.Context, team string) (result adminlessFeature, err error) {
	tid, err := postgresUUID(team)
	if err != nil {
		return result, invalidAdminlessFeature()
	}
	var row *featureMigrationRow
	switch s.Location {
	case storageCassandra:
		row, err = s.cassandraPatch(ctx, team)
	case storagePostgres:
		row, err = s.postgresPatch(ctx, team)
	case storageMigrating:
		if s.Postgres == nil {
			return result, errors.New("adminless feature Postgres unavailable")
		}
		err = s.Postgres.withMigrationLockKey(ctx, featureMigrationLockKey(tid, adminlessFeatureName), false, 500*time.Millisecond, func() error {
			var err error
			row, err = s.postgresPatch(ctx, team)
			if err != nil {
				return err
			}
			if row == nil {
				row, err = s.cassandraPatch(ctx, team)
			}
			return err
		})
	default:
		return result, errors.New("invalid adminless feature storage location")
	}
	if err != nil {
		return result, err
	}
	return resolveAdminlessFeature(s.Defaults, row)
}
