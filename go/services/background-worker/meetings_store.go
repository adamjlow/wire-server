// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"sort"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Wire.MeetingsStore/PostgresUnmarshall validates full selected rows before any
// deletion. Retain unused columns in decoding so malformed data cannot be hidden
// by projecting only meeting/conversation ids.
type storedMeeting struct {
	ID, Creator, Conversation    pgtype.UUID
	Title, TZ, Type              string
	Start, End, Created, Updated time.Time
	Frequency                    *string
	Interval                     *int32
	Until                        *time.Time
	Emails                       []string
	Trial                        bool
}

func (m storedMeeting) validate() error {
	invalid := errors.New("invalid stored meeting")
	n := utf8.RuneCountInString(m.Title)
	if !m.ID.Valid || !m.Creator.Valid || !m.Conversation.Valid || !utf8.ValidString(m.Title) || n < 1 || n > 256 {
		return invalid
	}
	if _, err := cronTimezone(&m.TZ); err != nil {
		return invalid
	}
	if m.Type != "immediate" && m.Type != "scheduled" {
		return invalid
	}
	if m.Frequency != nil {
		if m.Interval == nil {
			return invalid
		}
		switch *m.Frequency {
		case "daily", "weekly", "monthly", "yearly":
		default:
			return invalid
		}
		// Native stored-row decoder accepts signed interval values, including zero.
	}
	return nil
}
func (m storedMeeting) effectiveEnd() *time.Time {
	if m.Frequency == nil {
		return &m.End
	}
	if m.Until == nil {
		return nil
	}
	if m.End.After(*m.Until) {
		return &m.End
	}
	return m.Until
}

const meetingColumns = `id,title,creator,start_time,end_time,tzid,mtype::text,recurrence_frequency::text,recurrence_interval,recurrence_until,conversation_id,invited_emails,trial,created_at,updated_at`
const expiredNonRecurringMeetings = `SELECT ` + meetingColumns + ` FROM meetings
 WHERE recurrence_frequency IS NULL AND end_time<$1::timestamptz ORDER BY end_time ASC LIMIT $2::int4`
const expiredRecurringMeetings = `SELECT ` + meetingColumns + ` FROM meetings
 WHERE recurrence_frequency IS NOT NULL AND recurrence_interval IS NOT NULL AND recurrence_until IS NOT NULL
 AND GREATEST(end_time,recurrence_until)<$1::timestamptz ORDER BY GREATEST(end_time,recurrence_until) ASC LIMIT $2::int4`

func readExpiredMeetings(ctx context.Context, conn *pgx.Conn, sql string, cutoff time.Time, batch int32) ([]storedMeeting, error) {
	rows, err := conn.Query(ctx, sql, cutoff, batch)
	if err != nil {
		return nil, postgresFailure(err, "expired meeting query failed")
	}
	defer rows.Close()
	var result []storedMeeting
	for rows.Next() {
		var m storedMeeting
		if err := rows.Scan(&m.ID, &m.Title, &m.Creator, &m.Start, &m.End, &m.TZ, &m.Type, &m.Frequency, &m.Interval, &m.Until, &m.Conversation, &m.Emails, &m.Trial, &m.Created, &m.Updated); err != nil {
			return nil, postgresFailure(err, "expired meeting decode failed")
		}
		if err := m.validate(); err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	if err := rows.Err(); err != nil {
		return nil, postgresFailure(err, "expired meeting query failed")
	}
	return result, nil
}
func mergeExpiredMeetings(nonRecurring, recurring []storedMeeting, batch int) []storedMeeting {
	all := append(append([]storedMeeting(nil), nonRecurring...), recurring...)
	// Native sortOn is stable: tied effective ends retain branch order. Both
	// query predicates guarantee a bounded recurrence has an effective end.
	sort.SliceStable(all, func(i, j int) bool { return all[i].effectiveEnd().Before(*all[j].effectiveEnd()) })
	if len(all) > batch {
		all = all[:batch]
	}
	return all
}
func (s *postgresStore) OldMeetings(ctx context.Context, cutoff time.Time, batch int) (meetings []storedMeeting, err error) {
	if batch <= 0 {
		return nil, errors.New("invalid meeting cleanup batch size")
	}
	err = retryPostgres(ctx, s.Pool.Reset, func() error {
		meetings = nil
		conn, err := s.acquire(ctx)
		if err != nil {
			return err
		}
		defer conn.Release()
		// Native runSession runs two statements, not one OR scan or a new transaction.
		nonRecurring, err := readExpiredMeetings(ctx, conn.Conn(), expiredNonRecurringMeetings, cutoff, int32(batch))
		if err != nil {
			return err
		}
		recurring, err := readExpiredMeetings(ctx, conn.Conn(), expiredRecurringMeetings, cutoff, int32(batch))
		if err != nil {
			return err
		}
		meetings = mergeExpiredMeetings(nonRecurring, recurring, batch)
		return nil
	})
	return
}
func meetingCodeKey(id string) (string, error) {
	parsed, err := parseUserID(id)
	if err != nil {
		return "", errors.New("invalid meeting identifier")
	}
	// Native Code.mkKeyId hashes the textual UUID with the meeting: prefix,
	// truncates SHA256 to15 bytes and uses unpadded URL-safe base64.
	hash := sha256.Sum256([]byte("meeting:" + parsed.String()))
	return base64.RawURLEncoding.EncodeToString(hash[:15]), nil
}
func (s *postgresStore) DeleteMeetingCode(ctx context.Context, id string) error {
	key, err := meetingCodeKey(id)
	if err != nil {
		return err
	}
	return s.meetingDeleteStatement(ctx, `DELETE FROM conversation_codes WHERE key=$1::text`, key)
}
func (s *postgresStore) DeleteMeeting(ctx context.Context, id string) error {
	uuid, err := postgresUUID(id)
	if err != nil {
		return err
	}
	return s.meetingDeleteStatement(ctx, `DELETE FROM meetings WHERE id=$1::uuid`, uuid)
}
func (s *postgresStore) meetingDeleteStatement(ctx context.Context, sql string, arg any) error {
	return retryPostgres(ctx, s.Pool.Reset, func() error {
		conn, err := s.acquire(ctx)
		if err != nil {
			return err
		}
		defer conn.Release()
		if _, err := conn.Exec(ctx, sql, arg); err != nil {
			return postgresFailure(err, "meeting deletion failed")
		}
		return nil
	})
}

// Meetings always live in Postgres, but CodeStore follows the configured code
// storage mode. Native Cassandra has no meeting codes and explicitly does
// nothing for DeleteMeetingCode; migrating/Postgres delete the Postgres key.
type routedMeetingsCleanupStore struct {
	Postgres *postgresStore
	Codes    storageLocation
}

func (s routedMeetingsCleanupStore) OldMeetings(ctx context.Context, cutoff time.Time, batch int) ([]storedMeeting, error) {
	if s.Postgres == nil {
		return nil, errors.New("meeting storage unavailable")
	}
	return s.Postgres.OldMeetings(ctx, cutoff, batch)
}
func (s routedMeetingsCleanupStore) DeleteMeeting(ctx context.Context, id string) error {
	if s.Postgres == nil {
		return errors.New("meeting storage unavailable")
	}
	return s.Postgres.DeleteMeeting(ctx, id)
}
func (s routedMeetingsCleanupStore) DeleteMeetingCode(ctx context.Context, id string) error {
	switch s.Codes {
	case storageCassandra:
		return nil // Source no-op, not an unimplemented path.
	case storageMigrating, storagePostgres:
		if s.Postgres == nil {
			return errors.New("meeting code storage unavailable")
		}
		return s.Postgres.DeleteMeetingCode(ctx, id)
	default:
		return errors.New("invalid meeting code storage location")
	}
}
