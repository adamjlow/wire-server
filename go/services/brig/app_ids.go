// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"errors"
	"unicode/utf8"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/jackc/pgx/v5"
)

// Native AppStore is always PostgreSQL, independently of user migration mode.
// Keep native full-row decoding even though this endpoint returns only IDs.
func (s userAuthStore) teamAppIDs(ctx context.Context, team gocql.UUID) ([]string, error) {
	ids := []string{}
	err := s.withPostgres(ctx, func(conn *pgx.Conn) error {
		rows, err := conn.Query(ctx, `SELECT user_id::text,team_id::text,metadata,category,description,creator::text FROM apps WHERE team_id=$1::uuid`, team.String())
		if err != nil {
			return userStorageError(err)
		}
		defer rows.Close()
		for rows.Next() {
			var id, teamID, category, description, creator string
			var metadata []byte
			if err := rows.Scan(&id, &teamID, &metadata, &category, &description, &creator); err != nil {
				return userStorageError(err)
			}
			if err := validateAppIDRow(id, teamID, metadata, category, description, creator); err != nil {
				return err
			}
			ids = append(ids, id)
		}
		return userStorageError(rows.Err())
	})
	return ids, err
}

func validateAppIDRow(id, team string, metadata []byte, category, description, creator string) error {
	failure := errors.New("invalid stored app")
	for _, value := range []string{id, team, creator} {
		if _, valid := tokenUUID(value); !valid {
			return failure
		}
	}
	var object map[string]json.RawMessage
	if !utf8.Valid(metadata) || json.Unmarshal(metadata, &object) != nil || object == nil || !validRegistrationJSONText(metadata) {
		return failure
	}
	if !utf8.ValidString(category) || !utf8.ValidString(description) || utf8.RuneCountInString(description) > 300 {
		return failure
	}
	return nil
}
