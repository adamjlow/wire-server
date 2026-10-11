// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"

	"github.com/apache/cassandra-gocql-driver/v2"
	"golang.org/x/sync/errgroup"
)

type publicClient struct {
	ID    string  `json:"id"`
	Class *string `json:"class,omitempty"`
}
type publicClientMap map[string][]publicClient
type publicClientService struct {
	domain string
	remote *prekeyFederationClient
	list   func(context.Context, gocql.UUID) ([]publicClient, error)
	warn   func(string)
}

var publicClientClasses = []string{"phone", "tablet", "desktop", "legalhold"}

func publicClientSet(clients []publicClient) []publicClient {
	slices.SortFunc(clients, func(a, b publicClient) int {
		x, _ := strconv.ParseUint(a.ID, 16, 64)
		y, _ := strconv.ParseUint(b.ID, 16, 64)
		if x < y {
			return -1
		}
		if x > y {
			return 1
		}
		class := func(c *string) int {
			if c == nil {
				return -1
			}
			return slices.Index(publicClientClasses, *c)
		}
		return class(a.Class) - class(b.Class)
	})
	return slices.CompactFunc(clients, func(a, b publicClient) bool {
		return a.ID == b.ID && (a.Class == nil && b.Class == nil || a.Class != nil && b.Class != nil && *a.Class == *b.Class)
	})
}

func (s clientStore) publicClients(ctx context.Context, user gocql.UUID) ([]publicClient, error) {
	iter := s.cassandra.Query(`SELECT client, class from clients where user = ?`, user).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).Iter()
	result := []publicClient{}
	var raw *string
	var class *int32
	for iter.Scan(&raw, &class) {
		if raw == nil {
			_ = iter.Close()
			return nil, errors.New("invalid public client row")
		}
		id, err := parseClientID(*raw)
		if err != nil || class != nil && (*class < 0 || int(*class) >= len(publicClientClasses)) {
			_ = iter.Close()
			return nil, errors.New("invalid public client row")
		}
		client := publicClient{ID: id}
		if class != nil {
			value := publicClientClasses[*class]
			client.Class = &value
		}
		result = append(result, client)
	}
	if err := iter.Close(); err != nil {
		return nil, errors.New("public client lookup failed")
	}
	return publicClientSet(result), nil
}

func (s publicClientService) local(ctx context.Context, users []gocql.UUID) (publicClientMap, error) {
	values := make([][]publicClient, len(users))
	group, work := errgroup.WithContext(ctx)
	group.SetLimit(50)
	for i, user := range users {
		group.Go(func() error { var err error; values[i], err = s.list(work, user); return err })
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}
	result := publicClientMap{}
	for i, user := range users {
		result[user.String()] = values[i]
	}
	return result, nil
}

func decodePublicClientMap(raw json.RawMessage) (publicClientMap, error) {
	fields, err := registrationObject(raw)
	if err != nil {
		return nil, err
	}
	result := publicClientMap{}
	for user, value := range fields {
		id, ok := tokenUUID(user)
		if !ok {
			return nil, errors.New("invalid public client user")
		}
		var values []json.RawMessage
		if string(value) == "null" || json.Unmarshal(value, &values) != nil {
			return nil, errors.New("invalid public client set")
		}
		clients := []publicClient{}
		for _, value := range values {
			fields, err := registrationObject(value)
			if err != nil {
				return nil, err
			}
			text, err := registrationText(fields, "id", true)
			if err != nil {
				return nil, err
			}
			clientID, err := parseClientID(*text)
			if err != nil {
				return nil, err
			}
			class, err := registrationText(fields, "class", false)
			if err != nil || class != nil && !slices.Contains(publicClientClasses, *class) {
				return nil, errors.New("invalid public client class")
			}
			clients = append(clients, publicClient{ID: clientID, Class: class})
		}
		result[id] = publicClientSet(clients)
	}
	return result, nil
}

func (s publicClientService) qualified(ctx context.Context, domains map[string][]gocql.UUID, rid string) (map[string]publicClientMap, error) {
	result := map[string]publicClientMap{}
	names := []string{}
	for domain := range domains {
		if domain != s.domain {
			names = append(names, domain)
		}
	}
	slices.Sort(names)
	// Native deliberately fetches remote domains sequentially, swallowing each
	// federation failure, then reads local rows even with no requested local user.
	for _, domain := range names {
		if s.remote == nil {
			if s.warn != nil {
				s.warn(domain)
			}
			continue
		}
		ids := make([]string, len(domains[domain]))
		for i, user := range domains[domain] {
			ids[i] = user.String()
		}
		raw, err := s.remote.call(ctx, domain, "get-user-clients", rid, map[string]any{"users": ids})
		var clients publicClientMap
		if err == nil {
			clients, err = decodePublicClientMap(raw)
		}
		if err != nil {
			if s.warn != nil {
				s.warn(domain)
			}
			continue
		}
		result[domain] = clients
	}
	clients, err := s.local(ctx, domains[s.domain])
	if err != nil {
		return nil, err
	}
	result[s.domain] = clients
	return result, nil
}
