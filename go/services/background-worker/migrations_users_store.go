// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// Constructed only after native Cassandra decoding and handle-claim checks.
// A nil asset slice means absent; a present empty slice replaces PG assets.
type userMigrationRecord struct {
	ID                               pgtype.UUID
	AccentID                         int32
	Activated                        bool
	Country, Email, EmailUnvalidated *string
	Expires                          *time.Time
	ConferenceCalling                *int32
	Handle, Language                 *string
	ManagedBy                        *int32
	Name                             string
	Password                         *string
	Provider, Service, Team          pgtype.UUID
	SSO                              json.RawMessage
	Status, SupportedProtocols       *int32
	TextStatus                       *string
	UserType                         int32
	Picture, RichInfo                json.RawMessage
	Searchable                       *bool
	CreatedAt                        time.Time
	Assets                           []userMigrationAsset
	ServiceConversation              *userMigrationServiceConversation
}

type userMigrationAsset struct {
	Key  string
	Size *int32
}
type userMigrationServiceConversation struct{ Conversation, Team pgtype.UUID }

func (r userMigrationRecord) userValues(handle *string) []any {
	return []any{r.ID, r.AccentID, r.Activated, r.Country, r.Email, r.EmailUnvalidated, r.Expires, r.ConferenceCalling, handle, r.Language, r.ManagedBy, r.Name, r.Password, r.Provider, r.Service, r.SSO, r.Status, r.SupportedProtocols, r.Team, r.TextStatus, r.UserType, r.Picture, r.RichInfo, r.Searchable, r.CreatedAt}
}

type userMigrationSQL interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

// Exact native branch effects inside one Serializable transaction. No SQL
// interpolation and no password, handle or raw database error logging.
func writeUserMigration(ctx context.Context, tx userMigrationSQL, row userMigrationRecord) (removedHandle bool, err error) {
	if !row.ID.Valid || row.UserType < 0 || row.UserType > 2 {
		return false, errors.New("invalid user migration record")
	}
	exec := func(sql string, args ...any) error {
		if _, err := tx.Exec(ctx, sql, args...); err != nil {
			return postgresFailure(err, "user migration transaction write failed")
		}
		return nil
	}
	deleted := row.Status != nil && *row.Status == 2
	if deleted && row.UserType != 1 {
		if err := exec(`INSERT INTO deleted_user (id,team,created_at) VALUES ($1::uuid,$2::uuid,$3::timestamptz) ON CONFLICT (id) DO NOTHING`, row.ID, row.Team, row.CreatedAt); err != nil {
			return false, err
		}
	} else {
		handle := row.Handle
		if handle != nil {
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM wire_user WHERE handle=$2::text AND id!=$1::uuid)`, row.ID, *handle).Scan(&removedHandle); err != nil {
				return false, postgresFailure(err, "user migration handle query failed")
			}
			if removedHandle {
				handle = nil
			}
		}
		if err := exec(`INSERT INTO wire_user (id,accent_id,activated,country,email,email_unvalidated,expires,feature_conference_calling,handle,language,managed_by,name,password,provider,service,sso_id,account_status,supported_protocols,team,text_status,user_type,picture,rich_info,searchable,created_at) VALUES ($1::uuid,$2::integer,$3::boolean,$4::text,$5::text,$6::text,$7::timestamptz,$8::integer,$9::text,$10::text,$11::integer,$12::text,$13::text,$14::uuid,$15::uuid,$16::jsonb,$17::integer,$18::integer,$19::uuid,$20::text,$21::integer,$22::jsonb,$23::jsonb,$24::boolean,$25::timestamptz) ON CONFLICT (id) DO NOTHING`, row.userValues(handle)...); err != nil {
			return false, err
		}
		if row.Assets != nil {
			if err := exec(`DELETE FROM asset WHERE user_id=$1::uuid`, row.ID); err != nil {
				return false, err
			}
			ids := make([]pgtype.UUID, len(row.Assets))
			types := make([]int32, len(row.Assets))
			keys := make([]string, len(row.Assets))
			sizes := make([]*int32, len(row.Assets))
			for i, asset := range row.Assets {
				ids[i] = row.ID
				keys[i] = asset.Key
				sizes[i] = asset.Size
			}
			if err := exec(`INSERT INTO asset (user_id,typ,key,size) SELECT * FROM UNNEST($1::uuid[],$2::integer[],$3::text[],$4::integer[])`, ids, types, keys, sizes); err != nil {
				return false, err
			}
		}
		if !deleted && row.ServiceConversation != nil {
			bot := row.ServiceConversation
			if err := exec(`INSERT INTO bot_conv (id,conv,conv_team) VALUES ($1::uuid,$2::uuid,$3::uuid)`, row.ID, bot.Conversation, bot.Team); err != nil {
				return false, err
			}
		}
	}
	if err := exec(`INSERT INTO user_migration_pending_deletes (id) VALUES ($1::uuid) ON CONFLICT (id) DO NOTHING`, row.ID); err != nil {
		return false, err
	}
	return removedHandle, nil
}

func (s *postgresStore) saveUserMigration(ctx context.Context, row userMigrationRecord) (removedHandle bool, err error) {
	err = retryPostgres(ctx, s.Pool.Reset, func() error {
		conn, err := s.acquire(ctx)
		if err != nil {
			return err
		}
		defer conn.Release()
		removedHandle, err = retryUserMigrationTransaction(ctx, func() (bool, error) {
			tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable, AccessMode: pgx.ReadWrite})
			if err != nil {
				return false, postgresFailure(err, "user migration transaction start failed")
			}
			defer func() { _ = tx.Rollback(ctx) }()
			removed, err := writeUserMigration(ctx, tx, row)
			if err != nil {
				return false, err
			}
			if err := tx.Commit(ctx); err != nil {
				return false, postgresFailure(err, "user migration transaction commit failed")
			}
			return removed, nil
		})
		return err
	})
	return
}

// Hasql.Transaction.Sessions.transaction retries serialization failures and
// deadlocks within the same session, without delaying or rescanning other users.
func retryUserMigrationTransaction(ctx context.Context, attempt func() (bool, error)) (bool, error) {
	for {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		removed, err := attempt()
		var failure *postgresOperationError
		if !errors.As(err, &failure) || (failure.state != "40001" && failure.state != "40P01") {
			return removed, err
		}
	}
}

type userMigrationStore interface {
	saveUserMigration(context.Context, userMigrationRecord) (bool, error)
	completeUserMigration(context.Context, pgtype.UUID) error
}

func (s *postgresStore) completeUserMigration(ctx context.Context, id pgtype.UUID) error {
	return s.migrationExec(ctx, `DELETE FROM user_migration_pending_deletes WHERE id=$1::uuid`, id)
}

func copyUserMigration(ctx context.Context, row userMigrationRecord, store userMigrationStore, cleanup func(context.Context, userMigrationRecord) error) (bool, error) {
	removed, err := store.saveUserMigration(ctx, row)
	if err != nil {
		return false, err
	}
	if removed {
		slog.Warn("duplicate handle omitted from migrated user")
	}
	// Source cleanup uses the original validated Cassandra handle, even if a
	// duplicate PG claim caused the inserted user to lose that handle.
	if err := cleanup(ctx, row); err != nil {
		return false, err
	}
	if err := store.completeUserMigration(ctx, row.ID); err != nil {
		return false, err
	}
	return true, nil
}

type userMigrationDelete struct {
	CQL    string
	Values []any
}

func userMigrationDeletes(row userMigrationRecord) []userMigrationDelete {
	uid := gocql.UUID(row.ID.Bytes)
	var deletes []userMigrationDelete
	if row.Handle != nil {
		deletes = append(deletes, userMigrationDelete{`DELETE FROM user_handle WHERE handle = ?`, []any{*row.Handle}})
	}
	if row.Provider.Valid && row.Service.Valid {
		provider, service := gocql.UUID(row.Provider.Bytes), gocql.UUID(row.Service.Bytes)
		deletes = append(deletes, userMigrationDelete{`DELETE FROM service_user WHERE provider = ? AND service = ? AND user = ?`, []any{provider, service, uid}})
		if row.ServiceConversation != nil && row.ServiceConversation.Team.Valid {
			deletes = append(deletes, userMigrationDelete{`DELETE FROM service_team WHERE provider = ? AND service = ? AND team = ? AND user = ?`, []any{provider, service, gocql.UUID(row.ServiceConversation.Team.Bytes), uid}})
		}
	}
	return append(deletes, userMigrationDelete{`DELETE FROM rich_info WHERE user = ?`, []any{uid}}, userMigrationDelete{`DELETE FROM user WHERE id = ?`, []any{uid}})
}

func deleteMigratedUser(ctx context.Context, session *gocql.Session, row userMigrationRecord) error {
	for _, statement := range userMigrationDeletes(row) {
		// Native migration cleanup performs sequential LocalQuorum writes
		// without adding a batch or a retry policy beyond the client default.
		if err := session.Query(statement.CQL, statement.Values...).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(cassandraRetry(0)).Exec(); err != nil {
			return errors.New("Cassandra migrated user cleanup failed")
		}
	}
	return nil
}
