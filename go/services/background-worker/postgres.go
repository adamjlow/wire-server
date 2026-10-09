// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Source accepts libpq key/value options and a YAML string password file that
// overrides the inline password. Quote each value, never interpolate secrets
// into SQL, and never return the secret-bearing parser/connection error.
func postgresConfig(c PostgresPoolConfig, g GalleyConfig) (*pgxpool.Config, error) {
	invalid := errors.New("invalid PostgreSQL configuration")
	if c.Size < 1 || c.Size > math.MaxInt32 || c.Acquisition <= 0 || c.Idleness <= 0 || len(g.Postgres) == 0 {
		return nil, invalid
	}
	params := make(map[string]string, len(g.Postgres))
	keys := []string{}
	for key, value := range g.Postgres {
		if key == "" || strings.ContainsAny(key, "= \\'\t\n\r\x00") || strings.ContainsRune(value, '\x00') {
			return nil, invalid
		}
		params[key] = value
	}
	if g.PostgresPassword != nil {
		var password string
		if loadYAML(*g.PostgresPassword, &password) != nil || strings.ContainsRune(password, '\x00') {
			return nil, errors.New("PostgreSQL password file could not be read")
		}
		params["password"] = password
	}
	for key := range params {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	parts := []string{}
	for _, key := range keys {
		value := strings.ReplaceAll(strings.ReplaceAll(params[key], `\`, `\\`), `'`, `\'`)
		parts = append(parts, key+"='"+value+"'")
	}
	config, err := pgxpool.ParseConfig(strings.Join(parts, " "))
	if err != nil {
		return nil, invalid
	}
	config.MaxConns = int32(c.Size)
	config.MinConns = 0
	config.MaxConnIdleTime = time.Duration(c.Idleness)
	// Source has no connection lifetime retirement; retain idle retirement only.
	config.MaxConnLifetime = 0
	return config, nil
}

type postgresStore struct {
	Pool        *pgxpool.Pool
	Acquisition time.Duration
}

func openPostgres(ctx context.Context, c PostgresPoolConfig, g GalleyConfig) (*postgresStore, error) {
	config, err := postgresConfig(c, g)
	if err != nil {
		return nil, err
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, errors.New("PostgreSQL pool initialization failed")
	}
	// Hasql sessions round acquisition waits up to whole seconds.
	wait := time.Duration(c.Acquisition)
	seconds := wait / time.Second
	if wait%time.Second != 0 {
		seconds++
	}
	if seconds > time.Duration(math.MaxInt64)/time.Second {
		pool.Close()
		return nil, errors.New("invalid PostgreSQL acquisition timeout")
	}
	return &postgresStore{Pool: pool, Acquisition: seconds * time.Second}, nil
}

func (s *postgresStore) acquire(ctx context.Context) (*pgxpool.Conn, error) {
	acquireCtx, cancel := context.WithTimeout(ctx, s.Acquisition)
	defer cancel()
	conn, err := s.Pool.Acquire(acquireCtx)
	if err != nil {
		return nil, errors.New("PostgreSQL connection acquisition failed")
	}
	return conn, nil
}

func postgresUUID(value string) (pgtype.UUID, error) {
	u, err := parseUserID(value)
	if err != nil {
		return pgtype.UUID{}, errors.New("invalid PostgreSQL identifier")
	}
	return pgtype.UUID{Bytes: [16]byte(u), Valid: true}, nil
}

func postgresUUIDString(value pgtype.UUID) string {
	if !value.Valid {
		return ""
	}
	id := value.Bytes
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])
}

func (s *postgresStore) GroupChannels(ctx context.Context, team, group string) ([]string, bool, error) {
	tid, err := postgresUUID(team)
	if err != nil {
		return nil, false, err
	}
	gid, err := postgresUUID(group)
	if err != nil {
		return nil, false, err
	}
	conn, err := s.acquire(ctx)
	if err != nil {
		return nil, false, err
	}
	defer conn.Release()
	var ids []pgtype.UUID
	err = conn.QueryRow(ctx, `SELECT coalesce((SELECT array_agg(ugc.conv_id) FROM user_group_channel ugc WHERE ugc.user_group_id=ug.id),array[]::uuid[]) FROM user_group ug WHERE ug.id=$1::uuid AND ug.team_id=$2::uuid`, gid, tid).Scan(&ids)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, errors.New("user group channel query failed")
	}
	channels := make([]string, 0, len(ids))
	for _, id := range ids {
		channels = append(channels, postgresUUIDString(id))
	}
	return channels, true, nil
}

func (s *postgresStore) GroupMembers(ctx context.Context, team, group string) ([]string, bool, error) {
	tid, err := postgresUUID(team)
	if err != nil {
		return nil, false, err
	}
	gid, err := postgresUUID(group)
	if err != nil {
		return nil, false, err
	}
	conn, err := s.acquire(ctx)
	if err != nil {
		return nil, false, err
	}
	defer conn.Release()
	var name string
	var managedBy int32
	var created time.Time
	err = conn.QueryRow(ctx, `SELECT name,managed_by,created_at FROM user_group WHERE id=$1::uuid AND team_id=$2::uuid`, gid, tid).Scan(&name, &managedBy, &created)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, errors.New("user group metadata query failed")
	}
	// Metadata is decoded even though this job only consumes members. Match
	// the source name/managed-by validation rather than accepting corrupt rows.
	if len([]rune(name)) < 1 || len([]rune(name)) > 4000 || managedBy < 0 || managedBy > 1 {
		return nil, false, errors.New("invalid stored user group")
	}
	rows, err := conn.Query(ctx, `SELECT user_id FROM user_group_member WHERE user_group_id=$1::uuid`, gid)
	if err != nil {
		return nil, false, errors.New("user group member query failed")
	}
	defer rows.Close()
	users := []string{}
	for rows.Next() {
		var id pgtype.UUID
		if rows.Scan(&id) != nil || !id.Valid {
			return nil, false, errors.New("invalid stored group member")
		}
		users = append(users, postgresUUIDString(id))
	}
	if rows.Err() != nil {
		return nil, false, errors.New("user group member query failed")
	}
	return users, true, nil
}
