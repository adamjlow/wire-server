// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"context"
	"crypto/sha512"
	"encoding/base64"
	"errors"
	"github.com/apache/cassandra-gocql-driver/v2"
	"sort"
	"strings"
	"time"
)

type tokenInfo struct {
	Team        gocql.UUID  `json:"team"`
	ID          gocql.UUID  `json:"id"`
	CreatedAt   time.Time   `json:"created_at"`
	IdP         *gocql.UUID `json:"idp,omitempty"`
	Description string      `json:"description"`
	Name        string      `json:"name"`
}
type tokenRow struct {
	key  string
	info tokenInfo
}
type tokenStore interface {
	insertToken(context.Context, string, tokenInfo) error
	lookupToken(context.Context, string) (*tokenInfo, error)
	teamTokens(context.Context, gocql.UUID) ([]tokenInfo, error)
	deleteToken(context.Context, gocql.UUID, gocql.UUID) error
	renameToken(context.Context, gocql.UUID, gocql.UUID, string) error
	requireIdP(context.Context, gocql.UUID) error
	teamIdPs(context.Context, gocql.UUID) ([]gocql.UUID, error)
}

func tokenHash(token string) string {
	digest := sha512.Sum512([]byte(token))
	return "sha512:" + base64.StdEncoding.EncodeToString(digest[:])
}

const tokenColumns = "token_, team, id, created_at, idp, descr, name"
const insertTokenLookup = "INSERT INTO team_provisioning_by_token (token_, team, id, created_at, idp, descr, name) VALUES (?, ?, ?, ?, ?, ?, ?)"
const insertTokenTeam = "INSERT INTO team_provisioning_by_team (token_, team, id, created_at, idp, descr, name) VALUES (?, ?, ?, ?, ?, ?, ?)"

func (s cassandraStore) batch(ctx context.Context) *gocql.Batch {
	b := s.session.NewBatch(gocql.LoggedBatch).WithContext(ctx)
	b.Cons = gocql.LocalQuorum
	b.RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5})
	return b
}
func addToken(b *gocql.Batch, key string, info tokenInfo) {
	values := []any{key, info.Team, info.ID, info.CreatedAt, info.IdP, info.Description, info.Name}
	b.Query(insertTokenLookup, values...)
	b.Query(insertTokenTeam, values...)
}
func (s cassandraStore) insertToken(ctx context.Context, token string, info tokenInfo) error {
	b := s.batch(ctx)
	addToken(b, tokenHash(token), info)
	return s.session.ExecuteBatch(b)
}
func readTokenRows(query *gocql.Query) ([]tokenRow, error) {
	iter := query.Iter()
	rows := []tokenRow{}
	for {
		var row tokenRow
		var key, description, name *string
		var team, id *gocql.UUID
		var created *time.Time
		if !iter.Scan(&key, &team, &id, &created, &row.info.IdP, &description, &name) {
			break
		}
		if key == nil || team == nil || id == nil || created == nil || description == nil {
			_ = iter.Close()
			return nil, errors.New("invalid stored SCIM token row")
		}
		row.key = *key
		row.info.Team = *team
		row.info.ID = *id
		row.info.CreatedAt = *created
		row.info.Description = *description
		row.info.Name = row.info.ID.String()
		if name != nil {
			row.info.Name = *name
		}
		rows = append(rows, row)
	}
	return rows, iter.Close()
}

// This classification follows the native CQL lookup-key decoder. Prefix-bearing
// tokens are classified as hash keys; no caller-controlled key becomes SQL.
func chooseToken(rows []tokenRow) (*tokenRow, bool) {
	if len(rows) == 1 {
		return &rows[0], !strings.HasPrefix(rows[0].key, "sha512:")
	}
	if len(rows) == 2 {
		a, b := strings.HasPrefix(rows[0].key, "sha512:"), strings.HasPrefix(rows[1].key, "sha512:")
		if a != b {
			if a {
				return &rows[1], true
			}
			return &rows[0], true
		}
	}
	return nil, false
}
func (s cassandraStore) lookupToken(ctx context.Context, token string) (*tokenInfo, error) {
	rows, err := readTokenRows(s.read(ctx, "SELECT "+tokenColumns+" FROM team_provisioning_by_token WHERE token_ in (?, ?)", tokenHash(token), token))
	if err != nil {
		return nil, err
	}
	row, convert := chooseToken(rows)
	if row == nil {
		return nil, nil
	}
	if convert {
		b := s.batch(ctx)
		addToken(b, tokenHash(row.key), row.info)
		b.Query("DELETE FROM team_provisioning_by_token WHERE token_ = ?", row.key)
		if err = s.session.ExecuteBatch(b); err != nil {
			return nil, err
		}
	}
	return &row.info, nil
}
func (s cassandraStore) teamTokens(ctx context.Context, team gocql.UUID) ([]tokenInfo, error) {
	rows, err := readTokenRows(s.read(ctx, "SELECT "+tokenColumns+" FROM team_provisioning_by_team WHERE team = ?", team))
	if err != nil {
		return nil, err
	}
	infos := make([]tokenInfo, len(rows))
	for i, row := range rows {
		infos[i] = row.info
	}
	sort.SliceStable(infos, func(i, j int) bool { return infos[i].CreatedAt.Before(infos[j].CreatedAt) })
	return infos, nil
}
func (s cassandraStore) tokenKey(ctx context.Context, team, id gocql.UUID) (*string, error) {
	var key string
	err := s.read(ctx, "SELECT token_ FROM team_provisioning_by_team WHERE team = ? AND id = ?", team, id).Scan(&key)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, nil
	}
	return &key, err
}
func (s cassandraStore) deleteToken(ctx context.Context, team, id gocql.UUID) error {
	key, err := s.tokenKey(ctx, team, id)
	if err != nil {
		return err
	}
	b := s.batch(ctx)
	b.Query("DELETE FROM team_provisioning_by_team WHERE team = ? AND id = ?", team, id)
	if key != nil {
		b.Query("DELETE FROM team_provisioning_by_token WHERE token_ = ?", *key)
	}
	return s.session.ExecuteBatch(b)
}
func (s cassandraStore) renameToken(ctx context.Context, team, id gocql.UUID, name string) error {
	key, err := s.tokenKey(ctx, team, id)
	if err != nil {
		return err
	}
	b := s.batch(ctx)
	b.Query("UPDATE team_provisioning_by_team SET name = ? WHERE team = ? AND id = ?", name, team, id)
	if key != nil {
		b.Query("UPDATE team_provisioning_by_token SET name = ? WHERE token_ = ?", name, *key)
	}
	return s.session.ExecuteBatch(b)
}
func (s cassandraStore) teamIdPs(ctx context.Context, team gocql.UUID) ([]gocql.UUID, error) {
	iter := s.read(ctx, "SELECT idp FROM team_idp WHERE team = ?", team).Iter()
	ids := []gocql.UUID{}
	var id gocql.UUID
	for iter.Scan(&id) {
		ids = append(ids, id)
	}
	if err := iter.Close(); err != nil {
		return nil, err
	}
	// Native getConfigsByTeam resolves every referenced row before returning it.
	for _, id := range ids {
		if err := s.requireIdP(ctx, id); err != nil {
			return nil, err
		}
	}
	return ids, nil
}
