// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"context"
	"errors"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type scimCreation struct {
	UID      gocql.UUID
	Creating bool
}

func decodeSCIMCreation(uid *gocql.UUID, status *int32) (scimCreation, error) {
	if uid == nil || (status != nil && *status != 0 && *status != 1) {
		return scimCreation{}, errors.New("invalid stored SCIM creation state")
	}
	return scimCreation{*uid, status != nil && *status == 1}, nil
}
func (s cassandraStore) scimExternal(ctx context.Context, team gocql.UUID, external string) (*gocql.UUID, error) {
	var uid *gocql.UUID
	err := s.read(ctx, "SELECT user FROM scim_external WHERE team = ? and external_id = ?", team, external).Scan(&uid)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, nil
	}
	if err == nil && uid == nil {
		return nil, errors.New("invalid stored SCIM external identity")
	}
	return uid, err
}
func (s cassandraStore) scimCreation(ctx context.Context, team gocql.UUID, external string) (*scimCreation, error) {
	var uid *gocql.UUID
	var status *int32
	err := s.read(ctx, "SELECT user, creation_status FROM scim_external WHERE team = ? and external_id = ?", team, external).Scan(&uid, &status)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	value, err := decodeSCIMCreation(uid, status)
	if err != nil {
		return nil, err
	}
	return &value, nil
}
func (s cassandraStore) insertSCIMExternal(ctx context.Context, team gocql.UUID, external string, uid gocql.UUID) error {
	return s.write(ctx, "INSERT INTO scim_external (team, external_id, user) VALUES (?, ?, ?)", team, external, uid)
}
func (s cassandraStore) setSCIMCreation(ctx context.Context, team gocql.UUID, external string, uid gocql.UUID, creating bool) error {
	var status int32
	if creating {
		status = 1
	}
	return s.write(ctx, "INSERT INTO scim_external (team, external_id, user, creation_status) VALUES (?, ?, ?, ?)", team, external, uid, status)
}
func (s cassandraStore) deleteSCIMExternal(ctx context.Context, team gocql.UUID, external string) error {
	return s.write(ctx, "DELETE FROM scim_external WHERE team = ? and external_id = ?", team, external)
}

type scimUserMeta struct {
	Created, Updated time.Time
	EmailType        *string
	EmailPrimary     *bool
}

func decodeSCIMMeta(created, updated *time.Time, emailType *string, emailPrimary *bool) (scimUserMeta, error) {
	if created == nil || updated == nil {
		return scimUserMeta{}, errors.New("invalid stored SCIM user metadata")
	}
	return scimUserMeta{created.UTC(), updated.UTC(), emailType, emailPrimary}, nil
}
func (s cassandraStore) scimMeta(ctx context.Context, uid gocql.UUID) (*scimUserMeta, error) {
	var created, updated *time.Time
	var emailType *string
	var emailPrimary *bool
	err := s.read(ctx, "SELECT created_at, last_updated_at, email_type, email_primary FROM scim_user_times WHERE uid = ?", uid).Scan(&created, &updated, &emailType, &emailPrimary)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	meta, err := decodeSCIMMeta(created, updated, emailType, emailPrimary)
	if err != nil {
		return nil, err
	}
	return &meta, nil
}
func (s cassandraStore) scimMetaMany(ctx context.Context, ids []gocql.UUID) (map[gocql.UUID]scimUserMeta, error) {
	rows := s.read(ctx, "SELECT uid, created_at, last_updated_at, email_type, email_primary FROM scim_user_times WHERE uid IN ?", ids).Iter()
	result := map[gocql.UUID]scimUserMeta{}
	for {
		var uid *gocql.UUID
		var created, updated *time.Time
		var emailType *string
		var emailPrimary *bool
		if !rows.Scan(&uid, &created, &updated, &emailType, &emailPrimary) {
			break
		}
		meta, err := decodeSCIMMeta(created, updated, emailType, emailPrimary)
		if uid == nil || err != nil {
			_ = rows.Close()
			return nil, errors.New("invalid stored SCIM user metadata")
		}
		result[*uid] = meta
	}
	return result, rows.Close()
}
func (s cassandraStore) writeSCIMMeta(ctx context.Context, uid gocql.UUID, meta scimUserMeta) error {
	return s.write(ctx, "INSERT INTO scim_user_times (uid, created_at, last_updated_at, email_type, email_primary) VALUES (?, ?, ?, ?, ?)", uid, meta.Created.UTC().Truncate(time.Millisecond), meta.Updated.UTC().Truncate(time.Millisecond), meta.EmailType, meta.EmailPrimary)
}
func (s cassandraStore) deleteSCIMMeta(ctx context.Context, uid gocql.UUID) error {
	return s.write(ctx, "DELETE FROM scim_user_times WHERE uid = ?", uid)
}
