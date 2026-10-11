// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

// Filled by the native-shaped request decoder; password hashing and origin
// resolution happen before account construction, as in createUserWith.
type newRegistrationUser struct {
	Name                 string
	UUID                 *gocql.UUID
	Email                *string
	SSO                  json.RawMessage
	Picture              []json.RawMessage
	Assets               []userAsset
	Accent               *int32
	Locale               *registrationLocale
	ExpiresIn            *int64
	ManagedBy, Protocols *int32
}

func (newRegistrationUser) String() string { return "<NewRegistrationUser>" }

type registrationLocale struct {
	Language string
	Country  *string
}

func (n newRegistrationUser) build(invitation, team *gocql.UUID, password *string, defaultLocale registrationLocale, sessionTTL int64, now time.Time) (storedUser, error) {
	var id gocql.UUID
	switch {
	case invitation != nil:
		id = *invitation
	case n.UUID != nil:
		id = *n.UUID
	default:
		var err error
		id, err = gocql.RandomUUID()
		if err != nil {
			return storedUser{}, errors.New("registration randomness unavailable")
		}
	}
	typ, status, managed, protocols := int32(0), int32(0), int32(0), int32(1)
	if n.ManagedBy != nil {
		managed = *n.ManagedBy
	}
	if n.Protocols != nil {
		protocols = *n.Protocols
	}
	locale := defaultLocale
	if n.Locale != nil {
		locale = *n.Locale
	}
	var expires *time.Time
	if n.Email == nil && len(n.SSO) == 0 && managed == 0 {
		status = 3
		ttl := sessionTTL
		if n.ExpiresIn != nil {
			ttl = *n.ExpiresIn
		}
		// Do not silently wrap a configured expiry in time.Duration nanoseconds.
		if ttl <= 0 || now.Unix() > math.MaxInt64-ttl {
			return storedUser{}, errors.New("invalid registration expiry")
		}
		value := time.Unix(now.Unix()+ttl, int64(now.Nanosecond())).UTC().Truncate(time.Millisecond)
		expires = &value
	}
	accent := int32(0)
	if n.Accent != nil {
		accent = *n.Accent
	}
	searchable := true
	return storedUser{ID: id, Type: &typ, Name: n.Name, Picture: n.Picture, Assets: n.Assets, Email: n.Email, SSO: n.SSO, Accent: accent, Password: password, Activated: false, Status: &status, Expires: expires, Language: &locale.Language, Country: locale.Country, Team: team, ManagedBy: &managed, Protocols: &protocols, Searchable: &searchable}, nil
}

// Native NewStoredUser->User creation responses retain identity before stored
// activation. Their explicitly empty protocol set differs from stored reads,
// which historically default an empty set to proteus. Do not mutate storage.
func registrationProfile(account storedUser, domain, defaultLocale string) (selfProfile, error) {
	view := account
	view.Activated = true
	view.EmailUnvalidated = nil
	profile, err := view.selfProfile(domain, defaultLocale)
	if err != nil {
		return profile, err
	}
	if account.Protocols != nil && *account.Protocols&3 == 0 {
		profile.Protocols = []string{}
	}
	return profile, nil
}
