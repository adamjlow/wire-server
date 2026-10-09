// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"math/bits"
	"time"
	"unicode/utf8"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/jackc/pgx/v5/pgtype"
)

type featureMigrationRow struct {
	Team               pgtype.UUID
	Name               string
	Status, LockStatus *int32
	Config             json.RawMessage
}

func decodeFeatureMigration(row migrationRow) (featureMigrationRow, error) {
	var result featureMigrationRow
	invalid := errors.New("invalid stored team feature row")
	var ok bool
	result.Team, ok = cqlUUID(row["team"])
	if !ok || !result.Team.Valid {
		return result, invalid
	}
	result.Name, ok = row["feature"].(string)
	if !ok || !utf8.ValidString(result.Name) {
		return result, invalid
	}
	for column, target := range map[string]**int32{"status": &result.Status, "lock_status": &result.LockStatus} {
		if row[column] == nil {
			continue
		}
		n, ok := cqlInteger(row[column])
		if !ok || (n != 0 && n != 1) {
			return result, invalid
		}
		v := int32(n)
		*target = &v
	}
	if row["config"] != nil {
		value, ok := row["config"].(string)
		if !ok || !utf8.ValidString(value) || !json.Valid([]byte(value)) {
			return result, invalid
		}
		result.Config = json.RawMessage(value)
	}
	return result, nil
}

func featureMigrationLockKey(team pgtype.UUID, name string) int64 {
	w1, w2 := binary.BigEndian.Uint64(team.Bytes[:8]), binary.BigEndian.Uint64(team.Bytes[8:])
	return int64(w1 ^ (w2 >> 32) ^ (w2 << 32) ^ bits.RotateLeft64(uint64(textMigrationLockKey(name)), 1))
}

type featureMigrationStore interface {
	withFeatureLock(context.Context, featureMigrationRow, func() error) error
	featureExists(context.Context, featureMigrationRow) (bool, error)
	writeFeature(context.Context, featureMigrationRow) error
}

func (s *postgresStore) withFeatureLock(ctx context.Context, row featureMigrationRow, action func() error) error {
	return s.withMigrationLockKey(ctx, featureMigrationLockKey(row.Team, row.Name), true, 10*time.Second, action)
}

func (s *postgresStore) featureExists(ctx context.Context, row featureMigrationRow) (exists bool, err error) {
	err = retryPostgres(ctx, s.Pool.Reset, func() error {
		conn, err := s.acquire(ctx)
		if err != nil {
			return err
		}
		defer conn.Release()
		if err := conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM team_features WHERE team=$1::uuid AND feature=$2::text)`, row.Team, row.Name).Scan(&exists); err != nil {
			return postgresFailure(err, "team feature migration existence query failed")
		}
		return nil
	})
	return
}

func (s *postgresStore) writeFeature(ctx context.Context, row featureMigrationRow) error {
	return s.migrationExec(ctx, `INSERT INTO team_features (team,feature,status,lock_status,config) VALUES ($1::uuid,$2::text,$3::int,$4::int,$5::jsonb) ON CONFLICT (team,feature) DO UPDATE SET status=COALESCE(EXCLUDED.status,team_features.status),lock_status=COALESCE(EXCLUDED.lock_status,team_features.lock_status),config=COALESCE(EXCLUDED.config,team_features.config)`, row.Team, row.Name, row.Status, row.LockStatus, row.Config)
}

func (s *Service) copyTeamFeature(ctx context.Context, row migrationRow, store featureMigrationStore) (counted bool, err error) {
	value, err := decodeFeatureMigration(row)
	if err != nil {
		return false, err
	}
	err = s.timedExclusiveMigration(ctx, "team_features", func(ctx context.Context, action func() error) error { return store.withFeatureLock(ctx, value, action) }, func(ctx context.Context) error {
		exists, err := store.featureExists(ctx, value)
		if err != nil || exists {
			return err
		}
		if err := store.writeFeature(ctx, value); err != nil {
			return err
		}
		counted = true
		return nil
	})
	return
}

func (s *Service) migrateTeamFeatures(ctx context.Context, cassandra *gocql.Session, postgres *postgresStore) error {
	page := cassandraMigrationPage(cassandra, `SELECT team,feature,status,lock_status,config FROM team_features_dyn`, "team", "feature", "status", "lock_status", "config")
	page = validatedMigrationPage(page, func(row migrationRow) error { _, err := decodeFeatureMigration(row); return err })
	return s.migratePhases(ctx, "team_features", []migrationPhase{{page: page, write: func(ctx context.Context, row migrationRow) (bool, error) {
		return s.copyTeamFeature(ctx, row, postgres)
	}}})
}
