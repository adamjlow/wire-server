// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"context"
	"errors"
	"github.com/apache/cassandra-gocql-driver/v2"
)

func (s cassandraStore) idpIDs(ctx context.Context, query string, args ...any) ([]gocql.UUID, error) {
	iter := s.read(ctx, query, args...).Iter()
	ids := []gocql.UUID{}
	var id *gocql.UUID
	for iter.Scan(&id) {
		if id == nil {
			_ = iter.Close()
			return nil, errors.New("invalid stored IdP index")
		}
		ids = append(ids, *id)
	}
	return ids, iter.Close()
}
func (s cassandraStore) idpHandles(ctx context.Context, team gocql.UUID) ([]string, error) {
	ids, err := s.idpIDs(ctx, "SELECT idp FROM team_idp WHERE team = ?", team)
	if err != nil {
		return nil, err
	}
	iter := s.read(ctx, "SELECT handle FROM idp WHERE idp IN ?", ids).Iter()
	handles := []string{}
	var handle *string
	for iter.Scan(&handle) {
		if handle != nil {
			handles = append(handles, *handle)
		}
	}
	return handles, iter.Close()
}
func (s cassandraStore) getIdP(ctx context.Context, id gocql.UUID) (*idpConfig, error) {
	var team *gocql.UUID
	err := s.read(ctx, "SELECT team FROM idp WHERE idp = ?", id).Scan(&team)
	if err != nil && !errors.Is(err, gocql.ErrNotFound) {
		return nil, err
	}
	handles := []string{}
	if team != nil {
		handles, err = s.idpHandles(ctx, *team)
		if err != nil {
			return nil, err
		}
	}
	var stored *gocql.UUID
	var issuer, uri *string
	var head []byte
	var tail [][]byte
	var version *int32
	var old []string
	var replaced *gocql.UUID
	var handle, domain *string
	err = s.read(ctx, "SELECT idp, issuer, request_uri, public_key, extra_public_keys, team, api_version, old_issuers, replaced_by, handle, domain FROM idp WHERE idp = ?", id).Scan(&stored, &issuer, &uri, &head, &tail, &team, &version, &old, &replaced, &handle, &domain)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, errIdPNotFound
	}
	if err != nil {
		return nil, err
	}
	if stored == nil || team == nil || issuer == nil || uri == nil || !absoluteURI(*issuer) || !absoluteURI(*uri) {
		return nil, errors.New("invalid stored IdP row")
	}
	var apiVersion *string
	if version != nil {
		v := "v1"
		if *version == 2 {
			v = "v2"
		} else if *version != 1 {
			return nil, errors.New("invalid stored IdP version")
		}
		apiVersion = &v
	}
	if old == nil {
		old = []string{}
	}
	for _, previous := range old {
		if !absoluteURI(previous) {
			return nil, errors.New("invalid stored IdP issuer")
		}
	}
	info := &idpConfig{ID: *stored, Metadata: idpMetadata{Issuer: *issuer, RequestURI: *uri}, Extra: idpExtra{Team: *team, Version: apiVersion, OldIssuers: old, ReplacedBy: replaced, Handle: uniqueIdPHandle(handles), Domain: domain}}
	if handle != nil {
		info.Extra.Handle = *handle
	}
	for _, blob := range append([][]byte{head}, tail...) {
		cert, err := decodeStoredCertificate(blob)
		if err != nil {
			return nil, err
		}
		info.Metadata.Certificates = append(info.Metadata.Certificates, cert)
	}
	return info, nil
}
func (s cassandraStore) idpsByTeam(ctx context.Context, team gocql.UUID) ([]idpConfig, error) {
	ids, err := s.idpIDs(ctx, "SELECT idp FROM team_idp WHERE team = ?", team)
	if err != nil {
		return nil, err
	}
	infos := make([]idpConfig, 0, len(ids))
	for _, id := range ids {
		info, err := s.getIdP(ctx, id)
		if err != nil {
			return nil, err
		}
		infos = append(infos, *info)
	}
	return infos, nil
}
func mixedIdPVersion(expected int) error {
	message := "You cannot retrieve an API V2 IdP via API V1."
	if expected == 2 {
		message = "You cannot retrieve an API V1 IdP via API V2."
	}
	return &apiError{409, "cannot-mix-idp-api-verions", message}
}
func (s cassandraStore) idpByIssuer(ctx context.Context, issuer string, version int, team gocql.UUID) (*idpConfig, error) {
	query := "SELECT idp FROM issuer_idp_v2 WHERE issuer = ?"
	args := []any{issuer}
	if version == 2 {
		query += " and team = ?"
		args = append(args, team)
	}
	ids, err := s.idpIDs(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	if len(ids) > 1 {
		return nil, &apiError{409, "idp-non-unique", "We have found multiple IdPs with the same issuer. Please contact customer support."}
	}
	if len(ids) == 1 {
		info, err := s.getIdP(ctx, ids[0])
		if err != nil {
			return nil, err
		}
		if info.version() != version {
			return nil, mixedIdPVersion(version)
		}
		return info, nil
	}
	ids, err = s.idpIDs(ctx, "SELECT idp FROM issuer_idp WHERE issuer = ?", issuer)
	if err != nil {
		return nil, err
	}
	if len(ids) > 1 {
		return nil, &apiError{409, "idp-non-unique", "We have found multiple IdPs with the same issuer. Please contact customer support."}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	if version == 2 {
		return nil, mixedIdPVersion(2)
	}
	return s.getIdP(ctx, ids[0])
}
func (s cassandraStore) insertIdP(ctx context.Context, info idpConfig) error {
	// Native insert checks both old and v2 indexes, including other teams.
	first, err := s.idpIDs(ctx, "SELECT idp FROM issuer_idp WHERE issuer = ?", info.Metadata.Issuer)
	if err != nil {
		return err
	}
	second, err := s.idpIDs(ctx, "SELECT idp FROM issuer_idp_v2 WHERE issuer = ?", info.Metadata.Issuer)
	if err != nil {
		return err
	}
	for _, id := range append(first, second...) {
		existing, err := s.getIdP(ctx, id)
		if err != nil {
			return err
		}
		if existing.version() != info.version() {
			return &apiError{409, "cannot-mix-idp-api-verions", "You cannot have two IdPs with the same issuerwhere one of them is using API V1 and one API V2."}
		}
	}
	if len(info.Metadata.Certificates) == 0 {
		return errors.New("IdP has no signing certificates")
	}
	head := []byte(renderCertificate(info.Metadata.Certificates[0]))
	tail := make([][]byte, 0, len(info.Metadata.Certificates)-1)
	for _, cert := range info.Metadata.Certificates[1:] {
		tail = append(tail, []byte(renderCertificate(cert)))
	}
	var version *int32
	if info.Extra.Version != nil {
		v := int32(info.version())
		version = &v
	}
	b := s.batch(ctx)
	b.Query("INSERT INTO idp (idp, issuer, request_uri, public_key, extra_public_keys, team, api_version, old_issuers, replaced_by, handle, domain) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)", info.ID, info.Metadata.Issuer, info.Metadata.RequestURI, head, tail, info.Extra.Team, version, info.Extra.OldIssuers, info.Extra.ReplacedBy, info.Extra.Handle, info.Extra.Domain)
	b.Query("INSERT INTO issuer_idp_v2 (issuer, team, idp) VALUES (?, ?, ?)", info.Metadata.Issuer, info.Extra.Team, info.ID)
	b.Query("INSERT INTO team_idp (idp, team) VALUES (?, ?)", info.ID, info.Extra.Team)
	return s.session.ExecuteBatch(b)
}
func (s cassandraStore) rawIdP(ctx context.Context, id gocql.UUID) (*string, error) {
	var text *string
	err := s.read(ctx, "SELECT metadata FROM idp_raw_metadata WHERE id = ?", id).Scan(&text)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, nil
	}
	if err == nil && text == nil {
		return nil, errors.New("invalid stored raw metadata")
	}
	return text, err
}
func (s cassandraStore) storeRawIdP(ctx context.Context, id gocql.UUID, text string) error {
	return s.write(ctx, "INSERT INTO idp_raw_metadata (id, metadata) VALUES (?, ?)", id, text)
}
func (s cassandraStore) setIdPReplacement(ctx context.Context, old gocql.UUID, replacement *gocql.UUID) error {
	return s.write(ctx, "UPDATE idp SET replaced_by = ? WHERE idp = ?", replacement, old)
}
func (s cassandraStore) deleteIdPIssuer(ctx context.Context, issuer string, team *gocql.UUID) error {
	if team != nil {
		return s.write(ctx, "DELETE FROM issuer_idp_v2 WHERE issuer = ? AND team = ?", issuer, *team)
	}
	if err := s.write(ctx, "DELETE FROM issuer_idp WHERE issuer = ?", issuer); err != nil {
		return err
	}
	return s.write(ctx, "DELETE FROM issuer_idp_v2 WHERE issuer = ?", issuer)
}
func (s cassandraStore) deleteIdPConfig(ctx context.Context, info idpConfig) error {
	b := s.batch(ctx)
	b.Query("DELETE FROM default_idp WHERE partition_key_always_default = 'default' AND idp = ?", info.ID)
	b.Query("DELETE FROM idp WHERE idp = ?", info.ID)
	b.Query("DELETE FROM issuer_idp WHERE issuer = ?", info.Metadata.Issuer)
	b.Query("DELETE FROM issuer_idp_v2 WHERE issuer = ? AND team = ?", info.Metadata.Issuer, info.Extra.Team)
	b.Query("DELETE FROM team_idp WHERE team = ? and idp = ?", info.Extra.Team, info.ID)
	if err := s.session.ExecuteBatch(b); err != nil {
		return err
	}
	return s.write(ctx, "DELETE FROM idp_raw_metadata WHERE id = ?", info.ID)
}
