// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"github.com/apache/cassandra-gocql-driver/v2"
	"strings"
	"testing"
	"time"
)

func TestPublicProfilePrivacyPendingAndExpiry(t *testing.T) {
	id := gocql.TimeUUID()
	team := gocql.TimeUUID()
	email := "synthetic@example.com"
	password := "Synthetic-secret"
	pending := int32(4)
	ephemeral := int32(3)
	expires := time.Unix(5, 0)
	queued := 0
	user := storedUser{ID: id, Name: "Synthetic", Activated: true, Email: &email, Password: &password, Team: &team, SSO: json.RawMessage(`{"scim_external_id":"synthetic"}`)}
	s := publicProfileService{domain: "example.com", emailVisibility: "visible_to_self", get: func(context.Context, gocql.UUID) (*storedUser, error) { return &user, nil }, legalhold: func(context.Context, *storedUser, string) (string, error) { return "no_consent", nil }, enqueueDeletion: func(context.Context, gocql.UUID) error { queued++; return nil }, now: func() time.Time { return time.Unix(5, 0) }}
	profile, err := s.one(context.Background(), id, "req")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(profile)
	for _, field := range []string{"email", "password", "sso_id", "email_unvalidated", "locale", "managed_by", "status"} {
		var fields map[string]any
		_ = json.Unmarshal(raw, &fields)
		if _, present := fields[field]; present {
			t.Fatal("private field in public profile", field)
		}
	}
	user.Status = &pending
	if profile, err = s.one(context.Background(), id, "req"); err != nil || profile != nil {
		t.Fatal("pending invitation profile leaked")
	}
	user.Status = &ephemeral
	user.Expires = &expires
	if _, err = s.one(context.Background(), id, "req"); err != nil || queued != 0 {
		t.Fatal("equal expiry queued deletion")
	}
	s.now = func() time.Time { return time.Unix(6, 0) }
	if _, err = s.one(context.Background(), id, "req"); err != nil || queued != 1 {
		t.Fatal("expired user not queued", err)
	}
	s.emailVisibility = "visible_if_on_team"
	profile, err = s.one(context.Background(), id, "req")
	if err != nil || profile.Email == nil || *profile.Email != email {
		t.Fatal("configured team email visibility changed")
	}
	s.emailVisibility = "visible_if_on_same_team"
	profile, err = s.one(context.Background(), id, "req")
	if err != nil || profile.Email != nil {
		t.Fatal("viewerless same-team profile leaked email")
	}
}
func TestRemoteProfileRequiredFieldsAndTypedAdmission(t *testing.T) {
	valid := `[{"qualified_id":{"id":"00000000-0000-0000-0000-000000000001","domain":"b.example"},"name":"Synthetic","accent_id":1e0,"legalhold_status":"no_consent"}]`
	profiles, err := decodeRemoteUserProfiles(json.RawMessage(valid))
	if err != nil || len(profiles) != 1 || profiles[0].Type != "regular" || profiles[0].Protocols[0] != "proteus" {
		t.Fatal("native defaults/scientific integer changed", err)
	}
	for _, bad := range []string{`[{}]`, `null`, strings.Replace(valid, `"name":"Synthetic",`, "", 1), strings.Replace(valid, `1e0`, `1.5`, 1), strings.Replace(valid, `"no_consent"`, `"invalid"`, 1), strings.Replace(valid, `"b.example"`, `"bad/domain"`, 1), strings.Replace(valid, `"name":"Synthetic"`, `"name":""`, 1)} {
		if _, err := decodeRemoteUserProfiles(json.RawMessage(bad)); err == nil {
			t.Fatal("malformed remote profile accepted", bad)
		}
	}
}
