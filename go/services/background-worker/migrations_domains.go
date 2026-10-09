// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/jackc/pgx/v5/pgtype"
)

const domainRegistrationSelect = `SELECT domain,domain_redirect,team_invite,idp_id,backend_url,team,dns_verification_token,ownership_token_hash,authorized_team,webapp_url FROM domain_registration`

var domainRegistrationColumns = []string{"domain", "domain_redirect", "team_invite", "idp_id", "backend_url", "team", "dns_verification_token", "ownership_token_hash", "authorized_team", "webapp_url"}

type domainRegistrationMigrationRow struct {
	Domain string
	// The source typed tuple preserves nullable fields and enum validation.
	Values []any
}

func decodeDomainRegistrationMigration(row migrationRow) (domainRegistrationMigrationRow, error) {
	var result domainRegistrationMigrationRow
	invalid := errors.New("invalid stored domain registration row")
	domain, ok := row["domain"].(string)
	if !ok || !utf8.ValidString(domain) {
		return result, invalid
	}
	// DomainKey.fromCql is case-insensitive text, not Domain.mkDomain. Do not
	// strengthen its stored-data contract with API domain validation.
	result.Domain = foldedDomainKey(domain)
	result.Values = []any{result.Domain}
	for _, column := range domainRegistrationColumns[1:] {
		value := row[column]
		if value == nil {
			result.Values = append(result.Values, nil)
			continue
		}
		switch column {
		case "domain_redirect", "team_invite":
			n, ok := cqlInteger(value)
			max := int64(6)
			if column == "team_invite" {
				max = 3
			}
			if !ok || n < 1 || n > max {
				return result, invalid
			}
			value = int32(n)
		case "idp_id", "team", "authorized_team":
			id, ok := cqlUUID(value)
			if !ok || !id.Valid {
				return result, invalid
			}
			value = id
		case "backend_url", "webapp_url":
			blob, ok := value.([]byte)
			if !ok {
				return result, invalid
			}
			var err error
			value, err = storedHTTPSURL(blob)
			if err != nil {
				return result, invalid
			}
		case "dns_verification_token":
			token, ok := value.(string)
			if !ok || !base64URLAlphabet(token) {
				return result, invalid
			}
		case "ownership_token_hash":
			if _, ok := value.([]byte); !ok {
				return result, invalid
			}
		default:
			return result, invalid
		}
		result.Values = append(result.Values, value)
	}
	return result, nil
}

func base64URLAlphabet(value string) bool {
	// DnsVerificationToken.fromCql explicitly calls Ascii.validate. Unlike
	// generic AsciiText stored fields, this validation is part of its contract.
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '=') {
			return false
		}
	}
	return true
}

type domainChallengeMigrationRow struct {
	ID          pgtype.UUID
	Domain, DNS string
	Hash        []byte
	TTL         int32
}

func decodeDomainChallengeMigration(row migrationRow) (domainChallengeMigrationRow, error) {
	var result domainChallengeMigrationRow
	invalid := errors.New("invalid stored domain challenge row")
	var ok bool
	result.ID, ok = cqlUUID(row["id"])
	if !ok || !result.ID.Valid {
		return result, invalid
	}
	result.Domain, ok = row["domain"].(string)
	if !ok || !validDomain(result.Domain) {
		return result, invalid
	}
	result.Domain = strings.ToLower(result.Domain)
	result.DNS, ok = row["dns_verification_token"].(string)
	if !ok || !base64URLAlphabet(result.DNS) {
		return result, invalid
	}
	result.Hash, ok = row["challenge_token_hash"].([]byte)
	if !ok {
		return result, invalid
	}
	ttl, ok := cqlInteger(row["ttl(challenge_token_hash)"])
	if !ok || ttl < math.MinInt32 || ttl > math.MaxInt32 {
		return result, invalid
	}
	result.TTL = int32(ttl)
	return result, nil
}

func copyDomainChallenge(ctx context.Context, row migrationRow, exec func(context.Context, string, ...any) error) (bool, error) {
	value, err := decodeDomainChallengeMigration(row)
	if err != nil {
		return false, err
	}
	if value.TTL <= 0 {
		return false, nil
	}
	err = exec(ctx, `INSERT INTO domain_registration_challenge (id,domain,challenge_token_hash,dns_verification_token,expires_at) VALUES ($1::uuid,$2::text,$3::bytea,$4::text,now()+make_interval(secs=>$5::int)) ON CONFLICT (id) DO UPDATE SET domain=$2::text,challenge_token_hash=$3::bytea,dns_verification_token=$4::text,expires_at=now()+make_interval(secs=>$5::int)`, value.ID, value.Domain, value.Hash, value.DNS, value.TTL)
	return err == nil, err
}

type domainMigrationStore interface {
	withDomainLock(context.Context, string, func() error) error
	domainExists(context.Context, string) (bool, error)
	writeDomain(context.Context, domainRegistrationMigrationRow) error
}

func (s *postgresStore) withDomainLock(ctx context.Context, domain string, action func() error) error {
	return s.withMigrationLockKey(ctx, textMigrationLockKey(foldedDomainKey(domain)), true, 10*time.Second, action)
}

func (s *postgresStore) domainExists(ctx context.Context, domain string) (exists bool, err error) {
	err = retryPostgres(ctx, s.Pool.Reset, func() error {
		conn, err := s.acquire(ctx)
		if err != nil {
			return err
		}
		defer conn.Release()
		if err := conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM domain_registration WHERE domain=$1::text)`, domain).Scan(&exists); err != nil {
			return postgresFailure(err, "domain migration existence query failed")
		}
		return nil
	})
	return
}

func (s *postgresStore) writeDomain(ctx context.Context, value domainRegistrationMigrationRow) error {
	return s.migrationExec(ctx, `INSERT INTO domain_registration (domain,domain_redirect,team_invite,idp_id,backend_url,team,dns_verification_token,ownership_token_hash,authorized_team,webapp_url) VALUES ($1::text,$2::int,$3::int,$4::uuid,$5::bytea,$6::uuid,$7::text,$8::bytea,$9::uuid,$10::bytea) ON CONFLICT (domain) DO UPDATE SET domain_redirect=$2::int,team_invite=$3::int,idp_id=$4::uuid,backend_url=$5::bytea,team=$6::uuid,dns_verification_token=$7::text,ownership_token_hash=$8::bytea,authorized_team=$9::uuid,webapp_url=$10::bytea`, value.Values...)
}

func readCurrentDomain(ctx context.Context, cassandra *gocql.Session, domain string) (*domainRegistrationMigrationRow, error) {
	row := nullableCassandraRow(domainRegistrationColumns...)
	err := cassandra.Query(domainRegistrationSelect+` WHERE domain = ?`, domain).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(cassandraRetry(1)).MapScan(row)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.New("current Cassandra domain registration lookup failed")
	}
	unwrapCassandraRow(row)
	value, err := decodeDomainRegistrationMigration(migrationRow(row))
	if err != nil {
		return nil, err
	}
	return &value, nil
}

func (s *Service) copyDomainRegistration(ctx context.Context, row migrationRow, store domainMigrationStore, read func(context.Context, string) (*domainRegistrationMigrationRow, error)) (counted bool, err error) {
	value, err := decodeDomainRegistrationMigration(row)
	if err != nil {
		return false, err
	}
	err = s.timedExclusiveMigration(ctx, "domain_registration", func(ctx context.Context, action func() error) error {
		return store.withDomainLock(ctx, value.Domain, action)
	}, func(attempt context.Context) error {
		exists, err := store.domainExists(attempt, value.Domain)
		if err != nil || exists {
			return err
		}
		current, err := read(attempt, value.Domain)
		if err != nil || current == nil {
			return err
		}
		if err := store.writeDomain(attempt, *current); err != nil {
			return err
		}
		counted = true
		return nil
	})
	return
}

func (s *Service) migrateDomains(ctx context.Context, cassandra *gocql.Session, postgres *postgresStore) error {
	challenges := cassandraMigrationPage(cassandra, `SELECT id,domain,challenge_token_hash,dns_verification_token,ttl(challenge_token_hash) FROM domain_registration_challenge`, "id", "domain", "challenge_token_hash", "dns_verification_token", "ttl(challenge_token_hash)")
	challenges = validatedMigrationPage(challenges, func(row migrationRow) error { _, err := decodeDomainChallengeMigration(row); return err })
	registrations := cassandraMigrationPage(cassandra, domainRegistrationSelect, domainRegistrationColumns...)
	registrations = validatedMigrationPage(registrations, func(row migrationRow) error { _, err := decodeDomainRegistrationMigration(row); return err })
	return s.migratePhases(ctx, "domain_registration", []migrationPhase{
		{page: challenges, write: func(ctx context.Context, row migrationRow) (bool, error) {
			return copyDomainChallenge(ctx, row, postgres.migrationExec)
		}},
		{page: registrations, write: func(ctx context.Context, row migrationRow) (bool, error) {
			return s.copyDomainRegistration(ctx, row, postgres, func(ctx context.Context, domain string) (*domainRegistrationMigrationRow, error) {
				return readCurrentDomain(ctx, cassandra, domain)
			})
		}},
	})
}
