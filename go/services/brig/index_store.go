// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/jackc/pgx/v5"
)

type indexUser struct {
	user             storedUser
	created, updated time.Time
}

func (s userAuthStore) indexUser(ctx context.Context, user gocql.UUID) (*indexUser, error) {
	var result *indexUser
	err := s.withUserBackend(ctx, user, func(postgres bool) error {
		var err error
		if postgres {
			result, err = s.indexUserPostgres(ctx, user)
		} else {
			result, err = s.indexUserCassandra(ctx, user)
		}
		return err
	})
	return result, err
}

func (s userAuthStore) indexUserPostgres(ctx context.Context, user gocql.UUID) (*indexUser, error) {
	r := indexUser{user: storedUser{ID: user}}
	u := &r.user
	var rowID string
	var service, team *string
	err := s.withPostgres(ctx, func(conn *pgx.Conn) error {
		return conn.QueryRow(ctx, `SELECT id::text,team::text,name,account_status,handle,email,email_unvalidated,accent_id,activated,service::text,managed_by,sso_id,searchable,created_at,updated_at,user_type FROM wire_user WHERE id=$1::uuid`, user.String()).Scan(
			&rowID, &team, &u.Name, &u.Status, &u.Handle, &u.Email, &u.EmailUnvalidated, &u.Accent, &u.Activated, &service, &u.ManagedBy, &u.SSO, &u.Searchable, &r.created, &r.updated, &u.Type)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, userStorageError(err)
	}
	if u.Type == nil {
		return nil, errors.New("incomplete index user")
	}
	for _, field := range []struct {
		value  *string
		target **gocql.UUID
	}{{team, &u.Team}, {service, &u.Service}} {
		if field.value != nil {
			id, err := gocql.ParseUUID(*field.value)
			if err != nil {
				return nil, errors.New("invalid index user UUID")
			}
			*field.target = &id
		}
	}
	return &r, nil
}

func (s userAuthStore) indexUserCassandra(ctx context.Context, user gocql.UUID) (*indexUser, error) {
	r := indexUser{user: storedUser{ID: user}}
	u := &r.user
	var name *string
	var accent *int32
	var activated *bool
	var sso *string
	var times [13]*int64
	err := s.cassandra.Query(`SELECT id,user_type,team,writetime(team),name,writetime(name),status,writetime(status),handle,writetime(handle),email,writetime(email),accent_id,writetime(accent_id),activated,writetime(activated),service,writetime(service),managed_by,writetime(managed_by),sso_id,writetime(sso_id),email_unvalidated,writetime(email_unvalidated),searchable,writetime(searchable),writetime(write_time_bumper) FROM user WHERE id=?`, user).
		WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).Scan(
		&u.ID, &u.Type, &u.Team, &times[0], &name, &times[1], &u.Status, &times[2], &u.Handle, &times[3], &u.Email, &times[4], &accent, &times[5], &activated, &times[6], &u.Service, &times[7], &u.ManagedBy, &times[8], &sso, &times[9], &u.EmailUnvalidated, &times[10], &u.Searchable, &times[11], &times[12])
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, userStorageError(err)
	}
	if name == nil || accent == nil || activated == nil || times[1] == nil || times[5] == nil || times[6] == nil {
		return nil, errors.New("incomplete index user")
	}
	u.Name, u.Accent, u.Activated = *name, *accent, *activated
	if sso != nil {
		if !json.Valid([]byte(*sso)) {
			return nil, errors.New("invalid index SSO identity")
		}
		u.SSO = json.RawMessage(*sso)
	}
	if u.Type == nil {
		typ := int32(0)
		u.Type = &typ
	}
	r.created = time.UnixMicro(*times[6]).UTC()
	latest := int64(math.MinInt64)
	for _, timestamp := range times {
		if timestamp != nil {
			latest = max(latest, *timestamp)
		}
	}
	r.updated = time.UnixMicro(latest).UTC()
	return &r, nil
}

// The pinned Bloodhound DocVersion accepts 1 through 9.2e18, inclusive.
// Native mkIndexVersion falls back to minBound, which is the smallest valid
// DocVersion, not the signed integer minimum.
func indexDocumentVersion(updated time.Time, role *indexRole) int64 {
	version := updated.UnixMicro()
	if role != nil {
		version = max(version, role.written.UnixMicro())
	}
	if version <= 0 || version > 9200000000000000000 {
		return 1
	}
	return version
}
