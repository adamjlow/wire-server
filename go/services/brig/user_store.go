// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/jackc/pgx/v5"
)

type userAsset struct {
	Type int32  `cql:"typ"`
	Key  string `cql:"key"`
	Size *int32 `cql:"size"`
}

// Keep storage nullability separate from API defaults. Legacy rows may omit
// type/status/locale/protocols/searchability, and defaults differ by operation.
type storedUser struct {
	ID                      gocql.UUID
	Type                    *int32
	Name                    string
	TextStatus              *string
	Picture                 []json.RawMessage
	Assets                  []userAsset
	Email, EmailUnvalidated *string
	SSO                     json.RawMessage
	Accent                  int32
	Password                *string
	Activated               bool
	Status                  *int32
	Expires                 *time.Time
	Language, Country       *string
	Provider, Service, Team *gocql.UUID
	Handle                  *string
	ManagedBy, Protocols    *int32
	Searchable              *bool
}

func (storedUser) String() string { return "<StoredUser>" }

type userStore struct{ userAuthStore }

func uuidText(id *gocql.UUID) any {
	if id == nil {
		return nil
	}
	return id.String()
}

func (s userStore) get(ctx context.Context, user gocql.UUID) (*storedUser, error) {
	var result *storedUser
	if s.mode == "migration-to-postgresql" {
		err := s.withMigrationLockTimeout(ctx, user, 2*time.Second, func() error {
			cass, err := s.getCassandra(ctx, user)
			if err != nil {
				return err
			}
			pg, err := s.getPostgres(ctx, user)
			if err != nil {
				return err
			}
			result = pg
			if result == nil {
				result = cass
			}
			return nil
		})
		return result, err
	}
	err := s.withUserBackend(ctx, user, func(postgres bool) error {
		var err error
		if postgres {
			result, err = s.getPostgres(ctx, user)
		} else {
			result, err = s.getCassandra(ctx, user)
		}
		return err
	})
	return result, err
}

func (s userStore) getCassandra(ctx context.Context, user gocql.UUID) (*storedUser, error) {
	record := storedUser{ID: user}
	var pictures [][]byte
	var sso *string
	err := s.cassandra.Query(`SELECT user_type,name,text_status,picture,email,email_unvalidated,sso_id,accent_id,assets,activated,status,expires,language,country,provider,service,handle,team,managed_by,supported_protocols,searchable FROM user WHERE id=?`, user).
		WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).
		Scan(&record.Type, &record.Name, &record.TextStatus, &pictures, &record.Email, &record.EmailUnvalidated, &sso, &record.Accent, &record.Assets, &record.Activated, &record.Status, &record.Expires, &record.Language, &record.Country, &record.Provider, &record.Service, &record.Handle, &record.Team, &record.ManagedBy, &record.Protocols, &record.Searchable)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, userStorageError(err)
	}
	if err := decodeCassandraUser(&record, pictures, sso); err != nil {
		return nil, err
	}
	return &record, nil
}

func decodeCassandraUser(record *storedUser, pictures [][]byte, sso *string) error {
	for _, picture := range pictures {
		var object map[string]json.RawMessage
		if json.Unmarshal(picture, &object) != nil || object == nil {
			return errors.New("invalid stored picture")
		}
		record.Picture = append(record.Picture, json.RawMessage(picture))
	}
	if sso != nil {
		if !json.Valid([]byte(*sso)) {
			return errors.New("invalid stored SSO identity")
		}
		record.SSO = json.RawMessage(*sso)
	}
	for _, asset := range record.Assets {
		if asset.Type != 0 || (asset.Size != nil && *asset.Size != 0 && *asset.Size != 1) {
			return errors.New("invalid stored asset")
		}
	}
	return nil
}

func (s userStore) getPostgres(ctx context.Context, user gocql.UUID) (*storedUser, error) {
	record := storedUser{ID: user}
	err := s.withPostgres(ctx, func(conn *pgx.Conn) error {
		// Native lookup gives deleted_user priority even if a stale wire_user row
		// remains. Never expose that stale account or identity after deletion.
		var deleted bool
		var deletedTeam *string
		err := conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM deleted_user WHERE id=$1::uuid),(SELECT team::text FROM deleted_user WHERE id=$1::uuid)`, user.String()).Scan(&deleted, &deletedTeam)
		if err != nil {
			return err
		}
		if deleted {
			typ, status := int32(0), int32(2)
			record.Name = "default"
			record.Type = &typ
			record.Status = &status
			if deletedTeam != nil {
				id, e := gocql.ParseUUID(*deletedTeam)
				if e != nil {
					return e
				}
				record.Team = &id
			}
			return nil
		}
		var picture []byte
		var provider, service, team *string
		err = conn.QueryRow(ctx, `SELECT user_type,name,text_status,picture,email,email_unvalidated,sso_id,accent_id,activated,account_status,expires,language,country,provider::text,service::text,handle,team::text,managed_by,supported_protocols,searchable FROM wire_user WHERE id=$1::uuid`, user.String()).
			Scan(&record.Type, &record.Name, &record.TextStatus, &picture, &record.Email, &record.EmailUnvalidated, &record.SSO, &record.Accent, &record.Activated, &record.Status, &record.Expires, &record.Language, &record.Country, &provider, &service, &record.Handle, &team, &record.ManagedBy, &record.Protocols, &record.Searchable)
		if err != nil {
			return err
		}
		if picture != nil && json.Unmarshal(picture, &record.Picture) != nil {
			return errors.New("invalid stored picture")
		}
		for _, pair := range []struct {
			source *string
			target **gocql.UUID
		}{{provider, &record.Provider}, {service, &record.Service}, {team, &record.Team}} {
			if pair.source != nil {
				id, e := gocql.ParseUUID(*pair.source)
				if e != nil {
					return e
				}
				*pair.target = &id
			}
		}
		rows, err := conn.Query(ctx, `SELECT key,size FROM asset WHERE user_id=$1::uuid`, user.String())
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var asset userAsset
			if err := rows.Scan(&asset.Key, &asset.Size); err != nil {
				return err
			}
			record.Assets = append(record.Assets, asset)
		}
		return rows.Err()
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, userStorageError(err)
	}
	return &record, nil
}

func (s userStore) create(ctx context.Context, record storedUser, botConversation, botTeam *gocql.UUID) error {
	if s.mode == "migration-to-postgresql" {
		return s.withMigrationLock(ctx, record.ID, func() error {
			var existing gocql.UUID
			err := s.cassandra.Query(`SELECT id FROM user WHERE id=?`, record.ID).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).Scan(&existing)
			if err == nil {
				return s.createCassandra(ctx, record, botConversation, botTeam)
			}
			if !errors.Is(err, gocql.ErrNotFound) {
				return userStorageError(err)
			}
			return s.createPostgres(ctx, record, botConversation, botTeam)
		})
	}
	return s.withUserBackend(ctx, record.ID, func(postgres bool) error {
		if postgres {
			return s.createPostgres(ctx, record, botConversation, botTeam)
		}
		return s.createCassandra(ctx, record, botConversation, botTeam)
	})
}

func (s userStore) createCassandra(ctx context.Context, record storedUser, botConversation, botTeam *gocql.UUID) error {
	var picture [][]byte
	for _, object := range record.Picture {
		picture = append(picture, []byte(object))
	}
	var sso *string
	if len(record.SSO) > 0 {
		x := string(record.SSO)
		sso = &x
	}
	var password []byte
	if record.Password != nil {
		password = []byte(*record.Password)
	}
	batch := s.cassandra.NewBatch(gocql.LoggedBatch).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5})
	batch.Query(`INSERT INTO user (id,user_type,name,text_status,picture,assets,email,sso_id,accent_id,password,activated,status,expires,language,country,provider,service,team,managed_by,supported_protocols,searchable) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, record.ID, record.Type, record.Name, record.TextStatus, picture, record.Assets, record.Email, sso, record.Accent, password, record.Activated, record.Status, record.Expires, record.Language, record.Country, record.Provider, record.Service, record.Team, record.ManagedBy, record.Protocols, record.Searchable)
	if record.Service != nil && record.Provider != nil && botConversation != nil {
		batch.Query(`INSERT INTO service_user (provider,service,user,conv,team) VALUES (?,?,?,?,?)`, *record.Provider, *record.Service, record.ID, *botConversation, botTeam)
		if botTeam != nil {
			batch.Query(`INSERT INTO service_team (provider,service,user,conv,team) VALUES (?,?,?,?,?)`, *record.Provider, *record.Service, record.ID, *botConversation, *botTeam)
		}
	}
	return userStorageError(s.cassandra.ExecuteBatch(batch))
}

func (s userStore) createPostgres(ctx context.Context, record storedUser, botConversation, botTeam *gocql.UUID) error {
	picture := record.Picture
	if picture == nil {
		picture = []json.RawMessage{}
	}
	pictureJSON, err := json.Marshal(picture)
	if err != nil {
		return errors.New("invalid user picture")
	}
	err = s.withPostgres(ctx, func(conn *pgx.Conn) error {
		return migrationTransaction(ctx, conn, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `INSERT INTO wire_user (id,name,text_status,picture,email,sso_id,accent_id,password,activated,account_status,expires,language,country,provider,service,team,managed_by,supported_protocols,searchable,user_type) VALUES ($1::uuid,$2::text,$3::text,$4::jsonb,$5::text,$6::jsonb,$7::integer,$8::text,$9::boolean,$10::integer,$11::timestamptz,$12::text,$13::text,$14::uuid,$15::uuid,$16::uuid,$17::integer,$18::integer,$19::boolean,$20::integer) ON CONFLICT(id) DO UPDATE SET name=EXCLUDED.name,text_status=EXCLUDED.text_status,picture=EXCLUDED.picture,email=EXCLUDED.email,sso_id=EXCLUDED.sso_id,accent_id=EXCLUDED.accent_id,password=EXCLUDED.password,activated=EXCLUDED.activated,account_status=EXCLUDED.account_status,expires=EXCLUDED.expires,language=EXCLUDED.language,country=EXCLUDED.country,provider=EXCLUDED.provider,service=EXCLUDED.service,team=EXCLUDED.team,managed_by=EXCLUDED.managed_by,supported_protocols=EXCLUDED.supported_protocols,searchable=EXCLUDED.searchable,user_type=EXCLUDED.user_type`, record.ID.String(), record.Name, record.TextStatus, pictureJSON, record.Email, []byte(record.SSO), record.Accent, record.Password, record.Activated, record.Status, record.Expires, record.Language, record.Country, uuidText(record.Provider), uuidText(record.Service), uuidText(record.Team), record.ManagedBy, record.Protocols, record.Searchable, record.Type)
			if err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, `DELETE FROM asset WHERE user_id=$1::uuid`, record.ID.String()); err != nil {
				return err
			}
			for _, asset := range record.Assets {
				if _, err = tx.Exec(ctx, `INSERT INTO asset (user_id,typ,key,size) VALUES ($1::uuid,$2::integer,$3::text,$4::integer)`, record.ID.String(), int32(0), asset.Key, asset.Size); err != nil {
					return err
				}
			}
			if botConversation != nil {
				_, err = tx.Exec(ctx, `INSERT INTO bot_conv (id,conv,conv_team) VALUES ($1::uuid,$2::uuid,$3::uuid) ON CONFLICT (id) DO UPDATE SET conv=EXCLUDED.conv,conv_team=EXCLUDED.conv_team`, record.ID.String(), botConversation.String(), uuidText(botTeam))
			}
			return err
		})
	})
	return userStorageError(err)
}
