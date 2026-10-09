// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/jackc/pgx/v5/pgtype"
)

type observedFeatureMigrationStore struct {
	calls   []string
	exists  bool
	locked  bool
	written *featureMigrationRow
}

func (s *observedFeatureMigrationStore) withFeatureLock(_ context.Context, _ featureMigrationRow, action func() error) error {
	s.calls = append(s.calls, "lock")
	s.locked = true
	defer func() { s.locked = false; s.calls = append(s.calls, "unlock") }()
	return action()
}
func (s *observedFeatureMigrationStore) featureExists(context.Context, featureMigrationRow) (bool, error) {
	if !s.locked {
		return false, errors.New("feature existence read outside native lock")
	}
	s.calls = append(s.calls, "exists")
	return s.exists, nil
}
func (s *observedFeatureMigrationStore) writeFeature(_ context.Context, row featureMigrationRow) error {
	if !s.locked {
		return errors.New("feature write outside native lock")
	}
	s.calls = append(s.calls, "write")
	s.written = &row
	return nil
}

func TestFeatureMigrationPreservesExistingRowsAndNullablePatchUnderLock(t *testing.T) {
	team, _ := gocql.ParseUUID(jobUUID)
	for _, exists := range []bool{true, false} {
		s := testMigrationService(time.Second)
		store := &observedFeatureMigrationStore{exists: exists}
		counted, err := s.copyTeamFeature(t.Context(), migrationRow{"team": team, "feature": "unknown-future-feature", "status": int32(1)}, store)
		want := []string{"lock", "exists"}
		if !exists {
			want = append(want, "write")
		}
		want = append(want, "unlock")
		if err != nil || counted == exists || !reflect.DeepEqual(store.calls, want) {
			t.Fatal("native existing row preservation/lock ordering changed")
		}
		if counted && (store.written.Name != "unknown-future-feature" || *store.written.Status != 1 || store.written.LockStatus != nil || store.written.Config != nil) {
			t.Fatal("source nullable patch or forward-compatible feature name changed")
		}
		if s.migrations["team_features"].observations["success"].count != 1 {
			t.Fatal("successful skipped feature not observed")
		}
	}
}

func TestFeatureMigrationKeepsSQLNULLDistinctFromJSONNullAndRejectsMalformedRows(t *testing.T) {
	team, _ := gocql.ParseUUID(jobUUID)
	for _, value := range []any{nil, "null", `{"future_option":1e30}`, `[true,"synthetic"]`} {
		row, err := decodeFeatureMigration(migrationRow{"team": team, "feature": "", "config": value})
		if err != nil {
			t.Fatal("source generic JSON config or empty feature name rejected")
		}
		encoded, err := pgtype.NewMap().Encode(pgtype.JSONBOID, pgtype.TextFormatCode, row.Config, nil)
		if err != nil || (encoded == nil) != (value == nil) || (value == "null" && string(encoded) != "null") {
			t.Fatal("JSON null incorrectly became SQL NULL or vice versa")
		}
	}
	for column, value := range map[string]any{"team": nil, "feature": nil, "status": int32(2), "lock_status": int32(-1), "config": "invalid synthetic JSON"} {
		row := migrationRow{"team": team, "feature": "mls"}
		row[column] = value
		if _, err := decodeFeatureMigration(row); err == nil {
			t.Fatal("native typed feature decoding bypassed")
		}
	}
}

func TestFeatureMigrationLockKeyUsesNativeUUIDAndRotatedTextHash(t *testing.T) {
	team, err := postgresUUID("00000000-0000-4000-8000-000000000000")
	if err != nil {
		t.Fatal(err)
	}
	// Independently calculated from UUID words plus libxxhash XXH3_64bits.
	if uint64(featureMigrationLockKey(team, "mls")) != 0x8accdcdb4f74cf90 {
		t.Fatal("native feature lock combination changed")
	}
}
