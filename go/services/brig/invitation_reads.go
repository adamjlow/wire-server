// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"
	"time"
	"unicode/utf8"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type storedInvitation struct {
	Team, ID  gocql.UUID
	Role      *int32
	CreatedAt time.Time
	CreatedBy *gocql.UUID
	Email     string
	Name      *string
	Code      string
}

func (storedInvitation) String() string { return "<StoredInvitation>" }

type invitationStore struct{ cassandra *gocql.Session }

func asciiInvitationCode(value string) bool {
	// Native CQL AsciiBase64Url uses unsafeFromText: preserve its broader
	// stored ASCII acceptance instead of applying the stricter API decoder.
	for _, c := range value {
		if c > 127 {
			return false
		}
	}
	return true
}

func (s invitationStore) lookup(ctx context.Context, team, id gocql.UUID) (*storedInvitation, error) {
	var foundTeam, foundID *gocql.UUID
	var role *int32
	var createdAt *time.Time
	var createdBy *gocql.UUID
	var email, name, code *string
	err := s.cassandra.Query(`SELECT team,role,id,created_at,created_by,email,name,code FROM team_invitation WHERE team=? AND id=?`, team, id).
		WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).Scan(&foundTeam, &role, &foundID, &createdAt, &createdBy, &email, &name, &code)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, userStorageError(err)
	}
	if foundTeam == nil || foundID == nil || createdAt == nil || email == nil || code == nil || !asciiInvitationCode(*code) || role != nil && (*role < 1 || *role > 4) || name != nil && !utf8.ValidString(*name) {
		return nil, errors.New("invalid stored invitation")
	}
	canonical, err := canonicalRegistrationEmail(*email)
	if err != nil {
		return nil, errors.New("invalid stored invitation")
	}
	return &storedInvitation{Team: *foundTeam, ID: *foundID, Role: role, CreatedAt: createdAt.UTC(), CreatedBy: createdBy, Email: canonical, Name: name, Code: *code}, nil
}

func (s invitationStore) byEmail(ctx context.Context, email string) ([]storedInvitation, error) {
	rows := s.cassandra.Query(`SELECT team,invitation,code FROM team_invitation_email WHERE email=?`, email).
		WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).Iter()
	type reference struct{ team, id gocql.UUID }
	refs := []reference{}
	for {
		var team, id *gocql.UUID
		var code *string
		if !rows.Scan(&team, &id, &code) {
			break
		}
		if team == nil || id == nil || code == nil || !asciiInvitationCode(*code) {
			_ = rows.Close()
			return nil, errors.New("invalid stored invitation reference")
		}
		refs = append(refs, reference{*team, *id})
	}
	if err := rows.Close(); err != nil {
		return nil, userStorageError(err)
	}
	result := []storedInvitation{}
	// Native reads all references, including stale/malformed later rows, before
	// returning. Existence alone must not suppress a later decoding failure.
	for _, ref := range refs {
		invitation, err := s.lookup(ctx, ref.team, ref.id)
		if err != nil {
			return nil, err
		}
		if invitation != nil {
			result = append(result, *invitation)
		}
	}
	return result, nil
}

func (s invitationStore) byCode(ctx context.Context, code string) (*storedInvitation, error) {
	var team, id *gocql.UUID
	var storedCode *string
	err := s.cassandra.Query(`SELECT team,id,code FROM team_invitation_info WHERE code=?`, code).
		WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).Scan(&team, &id, &storedCode)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, userStorageError(err)
	}
	if team == nil || id == nil || storedCode == nil || !asciiInvitationCode(*storedCode) {
		return nil, errors.New("invalid stored invitation reference")
	}
	return s.lookup(ctx, *team, *id)
}

func (s invitationStore) delete(ctx context.Context, team, id gocql.UUID) error {
	// Native deletion reads only code/email: unrelated malformed invitation
	// metadata must not prevent removing the native rows.
	var code, email *string
	err := s.cassandra.Query(`SELECT code,email FROM team_invitation WHERE team=? AND id=?`, team, id).
		WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).Scan(&code, &email)
	if errors.Is(err, gocql.ErrNotFound) {
		return userStorageError(s.cassandra.Query(`DELETE FROM team_invitation WHERE team=? AND id=?`, team, id).
			WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5}).Exec())
	}
	if err != nil {
		return userStorageError(err)
	}
	if code == nil || email == nil || !asciiInvitationCode(*code) {
		return errors.New("invalid stored invitation deletion reference")
	}
	canonical, err := canonicalRegistrationEmail(*email)
	if err != nil {
		return errors.New("invalid stored invitation deletion reference")
	}
	batch := s.cassandra.NewBatch(gocql.LoggedBatch).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5})
	batch.Query(`DELETE FROM team_invitation WHERE team=? AND id=?`, team, id)
	batch.Query(`DELETE FROM team_invitation_info WHERE code=?`, *code)
	// Native removes the whole email/team partition, not only this ID.
	batch.Query(`DELETE FROM team_invitation_email WHERE email=? AND team=?`, canonical, team)
	return userStorageError(s.cassandra.ExecuteBatch(batch))
}

func (s invitationStore) removePending(ctx context.Context, user gocql.UUID) error {
	return userStorageError(s.cassandra.Query(`DELETE FROM users_pending_activation WHERE user IN ?`, []gocql.UUID{user}).
		WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5}).Exec())
}
