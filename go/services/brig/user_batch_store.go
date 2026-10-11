// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func (s userStore) getMany(ctx context.Context, ids []gocql.UUID) ([]storedUser, error) {
	if len(ids) == 0 {
		return []storedUser{}, nil
	}
	switch s.mode {
	case "cassandra":
		return s.getManyCassandra(ctx, ids)
	case "postgresql":
		return s.getManyPostgres(ctx, ids)
	case "migration-to-postgresql":
		var result []storedUser
		err := s.withUserMigrationLocks(ctx, ids, 2*time.Second, func() error {
			cass, err := s.getManyCassandra(ctx, ids)
			if err != nil {
				return err
			}
			pg, err := s.getManyPostgres(ctx, ids)
			if err != nil {
				return err
			}
			result = mergeMigratingUsers(ids, cass, pg)
			return nil
		})
		return result, err
	default:
		return nil, errors.New("invalid user storage mode")
	}
}

// Native Cassandra IN returns its partition order; preserve that order rather
// than inventing input-order sorting in Cassandra-only mode.
func (s userStore) getManyCassandra(ctx context.Context, ids []gocql.UUID) ([]storedUser, error) {
	rows := s.cassandra.Query(`SELECT id,user_type,name,text_status,picture,email,email_unvalidated,sso_id,accent_id,assets,activated,status,expires,language,country,provider,service,handle,team,managed_by,supported_protocols,searchable FROM user WHERE id IN ?`, ids).
		WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).Iter()
	result := []storedUser{}
	for {
		var record storedUser
		var pictures [][]byte
		var sso *string
		if !rows.Scan(&record.ID, &record.Type, &record.Name, &record.TextStatus, &pictures, &record.Email, &record.EmailUnvalidated, &sso, &record.Accent, &record.Assets, &record.Activated, &record.Status, &record.Expires, &record.Language, &record.Country, &record.Provider, &record.Service, &record.Handle, &record.Team, &record.ManagedBy, &record.Protocols, &record.Searchable) {
			break
		}
		if err := decodeCassandraUser(&record, pictures, sso); err != nil {
			_ = rows.Close()
			return nil, err
		}
		result = append(result, record)
	}
	if err := rows.Close(); err != nil {
		return nil, userStorageError(err)
	}
	return result, nil
}

func mergeMigratingUsers(ids []gocql.UUID, cass, pg []storedUser) []storedUser {
	byID := make(map[gocql.UUID]storedUser, len(cass)+len(pg))
	for _, user := range cass {
		byID[user.ID] = user
	}
	for _, user := range pg {
		// Including PostgreSQL deleted-user tombstones; never resurrect Cassandra.
		byID[user.ID] = user
	}
	result := []storedUser{}
	for _, id := range ids {
		if user, exists := byID[id]; exists {
			result = append(result, user)
		}
	}
	return result
}

// Native PostgreSQL returns Map.elems after deleted_user takes precedence.
// The pipeline below reads accounts, deleted rows and assets on one lease.
func (s userStore) getManyPostgres(ctx context.Context, ids []gocql.UUID) ([]storedUser, error) {
	uuids := make([]pgtype.UUID, len(ids))
	for i, id := range ids {
		uuids[i] = pgtype.UUID{Bytes: [16]byte(id), Valid: true}
	}
	result := []storedUser{}
	err := s.withPostgres(ctx, func(conn *pgx.Conn) error {
		batch := &pgx.Batch{}
		batch.Queue(`SELECT id::text,user_type,name,text_status,picture,email,email_unvalidated,sso_id,accent_id,activated,account_status,expires,language,country,provider::text,service::text,handle,team::text,managed_by,supported_protocols,searchable FROM wire_user WHERE id=ANY($1::uuid[])`, uuids)
		batch.Queue(`SELECT id::text,team::text FROM deleted_user WHERE id=ANY($1::uuid[])`, uuids)
		batch.Queue(`SELECT user_id::text,typ,key,size FROM asset WHERE user_id=ANY($1::uuid[])`, uuids)
		results := conn.SendBatch(ctx, batch)
		defer results.Close()
		byID := map[string]storedUser{}
		deleted := map[string]bool{}
		rows, err := results.Query()
		if err != nil {
			return err
		}
		for rows.Next() {
			var user storedUser
			var id string
			var picture []byte
			var provider, service, team *string
			if err := rows.Scan(&id, &user.Type, &user.Name, &user.TextStatus, &picture, &user.Email, &user.EmailUnvalidated, &user.SSO, &user.Accent, &user.Activated, &user.Status, &user.Expires, &user.Language, &user.Country, &provider, &service, &user.Handle, &team, &user.ManagedBy, &user.Protocols, &user.Searchable); err != nil {
				rows.Close()
				return err
			}
			user.ID, err = gocql.ParseUUID(id)
			if err != nil {
				rows.Close()
				return errors.New("invalid stored user ID")
			}
			if picture != nil && json.Unmarshal(picture, &user.Picture) != nil {
				rows.Close()
				return errors.New("invalid stored picture")
			}
			for _, pair := range []struct {
				source *string
				target **gocql.UUID
			}{{provider, &user.Provider}, {service, &user.Service}, {team, &user.Team}} {
				if pair.source != nil {
					id, err := gocql.ParseUUID(*pair.source)
					if err != nil {
						rows.Close()
						return errors.New("invalid stored user reference")
					}
					*pair.target = &id
				}
			}
			byID[id] = user
		}
		rows.Close()
		if rows.Err() != nil {
			return rows.Err()
		}
		rows, err = results.Query()
		if err != nil {
			return err
		}
		for rows.Next() {
			var id string
			var team *string
			if err := rows.Scan(&id, &team); err != nil {
				rows.Close()
				return err
			}
			uuid, err := gocql.ParseUUID(id)
			if err != nil {
				rows.Close()
				return err
			}
			typ, status := int32(0), int32(2)
			user := storedUser{ID: uuid, Name: "default", Type: &typ, Status: &status}
			if team != nil {
				tid, err := gocql.ParseUUID(*team)
				if err != nil {
					rows.Close()
					return err
				}
				user.Team = &tid
			}
			byID[id] = user
			deleted[id] = true
		}
		rows.Close()
		if rows.Err() != nil {
			return rows.Err()
		}
		rows, err = results.Query()
		if err != nil {
			return err
		}
		for rows.Next() {
			var id string
			var asset userAsset
			if err := rows.Scan(&id, &asset.Type, &asset.Key, &asset.Size); err != nil {
				rows.Close()
				return err
			}
			if user, found := byID[id]; found && !deleted[id] {
				user.Assets = append(user.Assets, asset)
				byID[id] = user
			}
		}
		rows.Close()
		if rows.Err() != nil {
			return rows.Err()
		}
		keys := make([]string, 0, len(byID))
		for id := range byID {
			keys = append(keys, id)
		}
		sort.Strings(keys)
		for _, id := range keys {
			result = append(result, byID[id])
		}
		return results.Close()
	})
	return result, userStorageError(err)
}
