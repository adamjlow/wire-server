// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"
	"sort"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type federationStaticDomain struct {
	Domain       *string `yaml:"domain"`
	SearchPolicy *string `yaml:"search_policy"`
}
type federationRemoteRestriction struct {
	Tag   string `json:"tag"`
	Value any    `json:"value"`
}
type federationRemoteDomain struct {
	Domain       string                      `json:"domain"`
	SearchPolicy string                      `json:"search_policy"`
	Restriction  federationRemoteRestriction `json:"restriction"`
}
type federationDomainConfigs struct {
	Strategy       string                   `json:"strategy"`
	Remotes        []federationRemoteDomain `json:"remotes"`
	UpdateInterval int                      `json:"update_interval"`
}
type federationRemoteRow struct {
	domain, searchPolicy string
	restriction          *int32
}
type federationConfigRead struct {
	strategy string
	static   []federationRemoteDomain
	rows     func(context.Context) ([]federationRemoteRow, error)
	teams    func(context.Context, string) ([]string, error)
}

func federationReadFromConfig(c Config) (federationConfigRead, error) {
	result := federationConfigRead{strategy: "allowNone", static: []federationRemoteDomain{}}
	invalid := errors.New("invalid federation configuration")
	if c.Settings.FederationStrategy != nil {
		result.strategy = *c.Settings.FederationStrategy
		switch result.strategy {
		case "allowNone", "allowAll", "allowDynamic":
		default:
			return result, invalid
		}
	}
	configured := map[string]string{}
	if c.Settings.FederationDomains != nil {
		for _, remote := range *c.Settings.FederationDomains {
			if remote.Domain == nil || remote.SearchPolicy == nil {
				return result, invalid
			}
			domain, err := parseEmailDomain(*remote.Domain)
			if err != nil {
				return result, invalid
			}
			policy := *remote.SearchPolicy
			switch policy {
			case "no_search", "exact_handle_search", "full_search":
			default:
				return result, invalid
			}
			if old, exists := configured[domain]; exists && old != policy {
				return result, invalid
			}
			configured[domain] = policy
		}
	}
	domains := make([]string, 0, len(configured))
	for domain := range configured {
		domains = append(domains, domain)
	}
	sort.Strings(domains)
	for _, domain := range domains {
		// Native ImplicitNoFederationRestriction ignores any restriction field
		// in static config and always produces allow_all with null value.
		result.static = append(result.static, federationRemoteDomain{Domain: domain, SearchPolicy: configured[domain], Restriction: federationRemoteRestriction{Tag: "allow_all"}})
	}
	return result, nil
}

func federationSearchPolicy(domain *string, policy *int32) (string, string, error) {
	invalid := errors.New("federation row decoding failed")
	if domain == nil || policy == nil {
		return "", "", invalid
	}
	parsed, err := parseEmailDomain(*domain)
	if err != nil {
		return "", "", invalid
	}
	name := ""
	switch *policy {
	case 0:
		name = "no_search"
	case 1:
		name = "exact_handle_search"
	case 2:
		name = "full_search"
	default:
		return "", "", invalid
	}
	return parsed, name, nil
}

func (s federationConfigRead) read(ctx context.Context) (federationDomainConfigs, error) {
	result := federationDomainConfigs{Strategy: s.strategy, Remotes: []federationRemoteDomain{}, UpdateInterval: 10}
	if s.rows == nil {
		return result, errors.New("federation store unavailable")
	}
	rows, err := s.rows(ctx)
	if err != nil {
		return result, err
	}
	for _, row := range rows {
		restriction := federationRemoteRestriction{Tag: "allow_all"}
		if row.restriction != nil && *row.restriction != 0 {
			if *row.restriction != 1 || s.teams == nil {
				return result, errors.New("federation restriction decoding failed")
			}
			teams, err := s.teams(ctx, row.domain)
			if err != nil {
				return result, err
			}
			if teams == nil {
				teams = []string{}
			}
			restriction = federationRemoteRestriction{Tag: "restrict_by_team", Value: teams}
		}
		result.Remotes = append(result.Remotes, federationRemoteDomain{Domain: row.domain, SearchPolicy: row.searchPolicy, Restriction: restriction})
	}
	// Native appends sorted file entries after DB results, preserving duplicates
	// across the two sources. Its read interpreter always returns interval10.
	result.Remotes = append(result.Remotes, s.static...)
	return result, nil
}

func (s *federationConfigRead) useCassandra(session *gocql.Session) {
	s.rows = func(ctx context.Context) ([]federationRemoteRow, error) {
		iter := session.Query(`SELECT domain, search_policy, restriction FROM federation_remotes LIMIT 10000`).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).Iter()
		var rows []federationRemoteRow
		var domain *string
		var policy, restriction *int32
		for iter.Scan(&domain, &policy, &restriction) {
			parsed, name, err := federationSearchPolicy(domain, policy)
			if err != nil {
				_ = iter.Close()
				return nil, err
			}
			var tag *int32
			if restriction != nil {
				value := *restriction
				tag = &value
			}
			rows = append(rows, federationRemoteRow{domain: parsed, searchPolicy: name, restriction: tag})
		}
		return rows, userStorageError(iter.Close())
	}
	s.teams = func(ctx context.Context, domain string) ([]string, error) {
		iter := session.Query(`SELECT team FROM federation_remote_teams WHERE domain = ?`, domain).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).Iter()
		teams := []string{}
		var team *gocql.UUID
		for iter.Scan(&team) {
			if team == nil {
				_ = iter.Close()
				return nil, errors.New("federation team decoding failed")
			}
			teams = append(teams, team.String())
		}
		return teams, userStorageError(iter.Close())
	}
}
