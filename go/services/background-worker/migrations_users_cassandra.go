// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"errors"
	"log/slog"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/jackc/pgx/v5/pgtype"
)

var userMigrationColumns = []string{
	"accent_id", "activated", "country", "email", "email_unvalidated",
	"expires", "feature_conference_calling", "handle", "language", "managed_by",
	"name", "password", "provider", "searchable", "service", "sso_id", "status",
	"supported_protocols", "team", "text_status", "user_type", "assets", "picture", "writetime(activated)",
}

const userMigrationSelect = `SELECT accent_id,activated,country,email,email_unvalidated,expires,feature_conference_calling,handle,language,managed_by,name,password,provider,searchable,service,sso_id,status,supported_protocols,team,text_status,user_type,assets,picture,writetime(activated) FROM user WHERE id = ?`
const userServiceConversationSelect = `SELECT conv,team FROM service_user WHERE provider = ? AND service = ? AND user = ?`
const userHandleClaimSelect = `SELECT user FROM user_handle WHERE handle = ?`
const userRichInfoSelect = `SELECT json FROM rich_info WHERE user = ?`

type userMigrationQuery func(context.Context, string, []string, ...any) (migrationRow, error)

func userMigrationCassandraQuery(session *gocql.Session) userMigrationQuery {
	return func(ctx context.Context, cql string, columns []string, args ...any) (migrationRow, error) {
		row := nullableCassandraRow(columns...)
		var assets *nullableUserMigrationAssets
		if _, present := row["assets"]; present {
			assets = &nullableUserMigrationAssets{}
			row["assets"] = assets
		}
		// Native getUserData uses query1 at LocalQuorum without x1/x5 retries.
		err := session.Query(cql, args...).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(cassandraRetry(0)).MapScan(row)
		if errors.Is(err, gocql.ErrNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, errors.New("Cassandra user migration lookup failed")
		}
		unwrapCassandraRow(row)
		if assets != nil {
			row["assets"] = assets.Value
		}
		return migrationRow(row), nil
	}
}

// Decode all typed source and related rows before the native missing-required-
// fields skip. A nil record denotes a disappeared or source-ineligible user.
func readUserMigration(ctx context.Context, id pgtype.UUID, query userMigrationQuery) (*userMigrationRecord, error) {
	row, err := query(ctx, userMigrationSelect, userMigrationColumns, gocql.UUID(id.Bytes))
	if err != nil || row == nil {
		return nil, err
	}
	record, eligible, err := decodeUserMigrationRow(id, row)
	if err != nil {
		return nil, err
	}
	var assets []userCassandraAsset
	if row["assets"] != nil {
		var ok bool
		assets, ok = row["assets"].([]userCassandraAsset)
		if !ok {
			return nil, invalidUserMigrationRow()
		}
	}
	if record.Provider.Valid && record.Service.Valid {
		service, err := query(ctx, userServiceConversationSelect, []string{"conv", "team"}, gocql.UUID(record.Provider.Bytes), gocql.UUID(record.Service.Bytes), gocql.UUID(id.Bytes))
		if err != nil {
			return nil, err
		}
		if service != nil {
			conv, ok := cqlUUID(service["conv"])
			if !ok || !conv.Valid {
				return nil, invalidUserMigrationRow()
			}
			bot := &userMigrationServiceConversation{Conversation: conv}
			if service["team"] != nil {
				team, ok := cqlUUID(service["team"])
				if !ok || !team.Valid {
					return nil, invalidUserMigrationRow()
				}
				bot.Team = team
			}
			record.ServiceConversation = bot
		}
	}
	claimValid := true
	if record.Handle != nil {
		claim, err := query(ctx, userHandleClaimSelect, []string{"user"}, *record.Handle)
		if err != nil {
			return nil, err
		}
		if claim == nil {
			claimValid = false
		} else {
			owner, ok := cqlUUID(claim["user"])
			if !ok || !owner.Valid {
				return nil, invalidUserMigrationRow()
			}
			claimValid = owner == id
		}
	}
	rich, err := query(ctx, userRichInfoSelect, []string{"json"}, gocql.UUID(id.Bytes))
	if err != nil {
		return nil, err
	}
	if rich != nil {
		blob, ok := rich["json"].([]byte)
		if !ok {
			return nil, invalidUserMigrationRow()
		}
		record.RichInfo, err = decodeUserMigrationRichInfo(blob)
		if err != nil {
			return nil, err
		}
	}
	if !eligible {
		slog.Warn("invalid stored user missing required fields; migration skipped")
		return nil, nil
	}
	if !claimValid {
		slog.Warn("unclaimed or differently claimed handle omitted from migrated user")
		record.Handle = nil
	}
	record.Assets = filterUserMigrationAssets(assets)
	return &record, nil
}
