// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/jackc/pgx/v5"
)

var (
	errInvalidEmailDomain         = errors.New("invalid email domain")
	errDomainRegistrationSSO      = errors.New("email domain redirects to SSO")
	errDomainRegistrationBackend  = errors.New("email domain redirects to another backend")
	errDomainRegistrationDisabled = errors.New("email domain registration disabled")
)

// All fields are decoded before interpreting tag combinations. Native typed
// decoding failures are errors; inconsistent combinations are ignored by lookup.
type domainRegistration struct {
	domain                    *string
	redirect, invite          *int32
	idp, team, authorizedTeam *gocql.UUID
	backend, webapp           []byte
	dnsToken                  *string
	ownershipHash             []byte
}

func (domainRegistration) String() string { return "<DomainRegistration>" }

type domainRegistrationStore struct {
	databases     userAuthStore
	mode          string
	invalidRecord func()
}

func parseEmailDomain(value string) (string, error) {
	if len(value) == 0 || len(value) > 253 {
		return "", errInvalidEmailDomain
	}
	labels := strings.Split(value, ".")
	if len(labels) < 2 && !strings.EqualFold(value, "localhost") {
		return "", errInvalidEmailDomain
	}
	for i, label := range labels {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", errInvalidEmailDomain
		}
		for _, c := range []byte(label) {
			if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return "", errInvalidEmailDomain
			}
		}
		if i == len(labels)-1 && label[0] >= '0' && label[0] <= '9' {
			return "", errInvalidEmailDomain
		}
	}
	return strings.ToLower(value), nil
}

func (r domainRegistration) decode() error {
	if r.domain == nil {
		return errors.New("domain registration decoding failed")
	}
	if _, err := parseEmailDomain(*r.domain); err != nil {
		return errors.New("domain registration decoding failed")
	}
	if r.redirect != nil && (*r.redirect < 1 || *r.redirect > 6) || r.invite != nil && (*r.invite < 1 || *r.invite > 3) {
		return errors.New("domain registration decoding failed")
	}
	for _, value := range [][]byte{r.backend, r.webapp} {
		if value == nil {
			continue
		}
		// Native HttpsUrl requires an absolute URI with exactly the lowercase
		// https scheme; it does not add a host-presence policy.
		parsed, err := url.Parse(string(value))
		if err != nil || !strings.HasPrefix(string(value), "https:") || parsed.Scheme != "https" {
			return errors.New("domain registration decoding failed")
		}
		for _, b := range value {
			if b <= 32 || b >= 127 {
				return errors.New("domain registration decoding failed")
			}
		}
	}
	if r.dnsToken != nil {
		for _, c := range *r.dnsToken {
			if c > 127 {
				return errors.New("domain registration decoding failed")
			}
		}
	}
	return nil
}
func (r domainRegistration) consistent() bool {
	invite := int32(1)
	if r.invite != nil {
		invite = *r.invite
	}
	if invite == 3 && r.team == nil || invite != 3 && r.team != nil {
		return false
	}
	redirect := int32(1)
	if r.redirect != nil {
		redirect = *r.redirect
	}
	switch redirect {
	case 1, 2, 5, 6:
		return r.idp == nil && r.backend == nil && r.webapp == nil
	case 3:
		return r.idp != nil && r.backend == nil && r.webapp == nil
	case 4:
		return r.idp == nil && r.backend != nil
	default:
		return false
	}
}
func (s domainRegistrationStore) lookup(ctx context.Context, domain string) (*domainRegistration, error) {
	key, err := parseEmailDomain(domain)
	if err != nil {
		return nil, err
	}
	var r domainRegistration
	switch s.mode {
	case "postgresql":
		var idp, team, authorized *string
		err = s.databases.withPostgres(ctx, func(conn *pgx.Conn) error {
			return conn.QueryRow(ctx, `SELECT domain,domain_redirect,team_invite,idp_id::text,backend_url,team::text,dns_verification_token,ownership_token_hash,authorized_team::text,webapp_url FROM domain_registration WHERE domain=$1`, key).Scan(&r.domain, &r.redirect, &r.invite, &idp, &r.backend, &team, &r.dnsToken, &r.ownershipHash, &authorized, &r.webapp)
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		if err == nil {
			for _, pair := range []struct {
				raw    *string
				target **gocql.UUID
			}{{idp, &r.idp}, {team, &r.team}, {authorized, &r.authorizedTeam}} {
				if pair.raw != nil {
					value, parseErr := gocql.ParseUUID(*pair.raw)
					if parseErr != nil {
						return nil, userStorageError(parseErr)
					}
					*pair.target = &value
				}
			}
		}
	case "cassandra", "migration-to-postgresql":
		// Native migration lookup is Cassandra-only, without migration locks.
		err = s.databases.cassandra.Query(`SELECT domain,domain_redirect,team_invite,idp_id,backend_url,team,dns_verification_token,ownership_token_hash,authorized_team,webapp_url FROM domain_registration WHERE domain=?`, key).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).Scan(&r.domain, &r.redirect, &r.invite, &r.idp, &r.backend, &r.team, &r.dnsToken, &r.ownershipHash, &r.authorizedTeam, &r.webapp)
		if errors.Is(err, gocql.ErrNotFound) {
			return nil, nil
		}
	default:
		return nil, errors.New("invalid domain registration storage mode")
	}
	if err != nil {
		return nil, userStorageError(err)
	}
	if err := r.decode(); err != nil {
		return nil, err
	}
	if !r.consistent() {
		if s.invalidRecord != nil {
			s.invalidRecord()
		}
		return nil, nil
	}
	return &r, nil
}

func guardRegistrationDomain(ctx context.Context, email string, lookup func(context.Context, string) (*domainRegistration, error)) error {
	at := strings.LastIndexByte(email, '@')
	if at < 0 {
		return errInvalidEmailDomain
	}
	domain, err := parseEmailDomain(email[at+1:])
	if err != nil {
		return err
	}
	record, err := lookup(ctx, domain)
	if err != nil || record == nil {
		return err
	}
	if record.redirect == nil {
		return nil
	}
	switch *record.redirect {
	case 3:
		return errDomainRegistrationSSO
	case 4:
		return errDomainRegistrationBackend
	case 5:
		return errDomainRegistrationDisabled
	default:
		return nil
	}
}
