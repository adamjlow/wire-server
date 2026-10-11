// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func TestRegistrationAccountDefaultsAndEphemeralExpiry(t *testing.T) {
	now := time.Date(2026, 10, 11, 1, 2, 3, 123456789, time.UTC)
	requested, _ := gocql.ParseUUID("00000000-0000-0000-0000-000000000001")
	invited, _ := gocql.ParseUUID("00000000-0000-0000-0000-000000000002")
	team, _ := gocql.ParseUUID("00000000-0000-0000-0000-000000000003")
	n := newRegistrationUser{Name: "Synthetic registration", UUID: &requested}
	account, err := n.build(&invited, &team, nil, registrationLocale{Language: "en"}, 30, now)
	if err != nil || account.ID != invited || account.Type == nil || *account.Type != 0 || account.Status == nil || *account.Status != 3 || account.Activated || account.Accent != 0 || account.Protocols == nil || *account.Protocols != 1 || account.ManagedBy == nil || *account.ManagedBy != 0 || account.Searchable == nil || !*account.Searchable || account.Expires == nil || !account.Expires.Equal(now.Add(30*time.Second).Truncate(time.Millisecond)) || account.Team == nil || *account.Team != team {
		t.Fatal("native defaults/precedence/expiry changed", err)
	}
	ttl := int64(2)
	n.ExpiresIn = &ttl
	account, err = n.build(nil, nil, nil, registrationLocale{Language: "en"}, 30, now)
	if err != nil || account.ID != requested || !account.Expires.Equal(now.Add(2*time.Second).Truncate(time.Millisecond)) {
		t.Fatal("explicit expiry/UUID changed", err)
	}
	scim := int32(1)
	n.ManagedBy = &scim
	account, err = n.build(nil, nil, nil, registrationLocale{Language: "en"}, 30, now)
	if err != nil || *account.Status != 0 || account.Expires != nil {
		t.Fatal("identityless SCIM account became ephemeral", err)
	}
}
func TestRegistrationCreationViewPreservesUnactivatedIdentityAndEmptyProtocols(t *testing.T) {
	email, password := "synthetic@example.com", "synthetic-encoded-password"
	country := "US"
	protocols := int32(0)
	accent := int32(3)
	n := newRegistrationUser{Name: "Synthetic user", Email: &email, Protocols: &protocols, Accent: &accent, SSO: json.RawMessage(`{"scim_external_id":"synthetic"}`), Locale: &registrationLocale{Language: "en", Country: &country}}
	account, err := n.build(nil, nil, &password, registrationLocale{Language: "de"}, 30, time.Now())
	if err != nil || *account.Status != 0 || account.Expires != nil || account.Password == nil || *account.Password != password || account.Activated {
		t.Fatal("native identified account creation changed", err)
	}
	created, err := registrationProfile(account, "example.com", "de")
	if err != nil || created.Email == nil || *created.Email != email || len(created.SSO) == 0 || len(created.Protocols) != 0 || created.Locale != "en-US" || created.Accent != 3 {
		t.Fatal("creation identity/protocol view changed", err)
	}
	stored, err := account.selfProfile("example.com", "de")
	if err != nil || stored.Email != nil || len(stored.SSO) != 0 || len(stored.Protocols) != 1 || account.Activated {
		t.Fatal("creation view mutated stored activation/defaults", err)
	}
	if n.String() != "<NewRegistrationUser>" {
		t.Fatal("registration input not redacted")
	}
}
