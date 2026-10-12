// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/pem"
	"errors"
	"math/bits"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/apache/cassandra-gocql-driver/v2"
)

var providerServiceTags = []string{"audio", "books", "business", "design", "education", "entertainment", "finance", "fitness", "food-drink", "games", "graphics", "health", "integration", "lifestyle", "media", "medical", "movies", "music", "news", "photography", "poll", "productivity", "quiz", "rating", "shopping", "social", "sports", "travel", "tutorial", "video", "weather"}

type providerServiceFields struct {
	ID          gocql.UUID     `json:"id"`
	Name        string         `json:"name"`
	Summary     string         `json:"summary"`
	Description string         `json:"description"`
	Assets      []profileAsset `json:"assets"`
	Tags        []string       `json:"tags"`
	Enabled     bool           `json:"enabled"`
}

type providerServiceProfile struct {
	providerServiceFields
	Provider gocql.UUID `json:"provider"`
}

type providerServicePublicKey struct {
	Type string `json:"type"`
	Size int32  `json:"size"`
	PEM  string `json:"pem"`
}

type providerServiceDefinition struct {
	providerServiceFields
	BaseURL string                     `json:"base_url"`
	Tokens  []string                   `json:"auth_tokens"`
	Keys    []providerServicePublicKey `json:"public_keys"`
}

func (providerServiceDefinition) String() string { return "<ProviderServiceDefinition>" }

func (r deletionServiceRow) profile(id gocql.UUID) (providerServiceFields, error) {
	var result providerServiceFields
	invalid := errors.New("invalid stored service profile")
	if r.name == nil || r.description == nil || r.enabled == nil || !utf8.ValidString(*r.name) || !utf8.ValidString(*r.description) || r.summary != nil && !utf8.ValidString(*r.summary) {
		return result, invalid
	}
	result = providerServiceFields{ID: id, Name: *r.name, Description: *r.description, Enabled: *r.enabled, Assets: []profileAsset{}, Tags: []string{}}
	if r.summary != nil {
		result.Summary = *r.summary
	}
	for _, asset := range r.assets {
		if asset.Type == nil || *asset.Type != 0 || asset.Key == nil {
			return result, invalid
		}
		key, valid := canonicalRegistrationAssetKey(*asset.Key)
		if !valid {
			return result, invalid
		}
		value := profileAsset{Type: "image", Key: key}
		if asset.Size != nil {
			size := "preview"
			if *asset.Size == 1 {
				size = "complete"
			} else if *asset.Size != 0 {
				return result, invalid
			}
			value.Size = &size
		}
		result.Assets = append(result.Assets, value)
	}
	tags := append([]int64(nil), r.tags...)
	sort.Slice(tags, func(i, j int) bool { return tags[i] < tags[j] })
	var last int64
	for _, tag := range tags {
		if tag <= 0 || tag > 1<<30 || tag&(tag-1) != 0 {
			return result, invalid
		}
		if tag != last {
			result.Tags = append(result.Tags, providerServiceTags[bits.TrailingZeros64(uint64(tag))])
		}
		last = tag
	}
	return result, nil
}

func (r deletionServiceRow) definition(id gocql.UUID) (providerServiceDefinition, error) {
	var result providerServiceDefinition
	if err := r.validate(); err != nil {
		return result, err
	}
	fields, err := r.profile(id)
	if err != nil {
		return result, err
	}
	result = providerServiceDefinition{providerServiceFields: fields, BaseURL: string(r.baseURL), Tokens: append([]string(nil), r.tokens...), Keys: []providerServicePublicKey{}}
	for _, token := range r.tokens {
		if !validActivationValue(token) {
			return result, errors.New("invalid stored service token")
		}
	}
	for _, key := range r.keys {
		block, rest := pem.Decode(key.PEM)
		if block == nil || len(strings.TrimSpace(string(rest))) != 0 {
			return result, errors.New("invalid stored service key")
		}
		result.Keys = append(result.Keys, providerServicePublicKey{Type: "rsa", Size: *key.Size, PEM: string(pem.EncodeToMemory(block))})
	}
	return result, nil
}

func (s serviceDeletionStore) serviceGet(ctx context.Context, provider, id gocql.UUID, public bool) (any, error) {
	var row deletionServiceRow
	query := `SELECT name,summary,descr,base_url,auth_tokens,pubkeys,assets,tags,enabled FROM service WHERE provider=? AND id=?`
	fields := []any{&row.name, &row.summary, &row.description, &row.baseURL, &row.tokens, &row.keys, &row.assets, &row.tags, &row.enabled}
	if public {
		query = `SELECT name,summary,descr,assets,tags,enabled FROM service WHERE provider=? AND id=?`
		fields = []any{&row.name, &row.summary, &row.description, &row.assets, &row.tags, &row.enabled}
	}
	err := s.cassandra.Query(query, provider, id).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).Scan(fields...)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, userStorageError(err)
	}
	if public {
		profile, err := row.profile(id)
		return providerServiceProfile{providerServiceFields: profile, Provider: provider}, err
	}
	return row.definition(id)
}

func (s serviceDeletionStore) serviceList(ctx context.Context, provider gocql.UUID, public bool) ([]any, error) {
	var row deletionServiceRow
	var id *gocql.UUID
	query := `SELECT id,name,summary,descr,base_url,auth_tokens,pubkeys,assets,tags,enabled FROM service WHERE provider=?`
	fields := []any{&id, &row.name, &row.summary, &row.description, &row.baseURL, &row.tokens, &row.keys, &row.assets, &row.tags, &row.enabled}
	consistency := gocql.LocalQuorum
	if public {
		query = `SELECT id,name,summary,descr,assets,tags,enabled FROM service WHERE provider=?`
		fields = []any{&id, &row.name, &row.summary, &row.description, &row.assets, &row.tags, &row.enabled}
		consistency = gocql.One
	}
	iter := s.cassandra.Query(query, provider).WithContext(ctx).Consistency(consistency).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).PageSize(0).Iter()
	result := []any{}
	for iter.Scan(fields...) {
		if id == nil {
			_ = iter.Close()
			return nil, errors.New("invalid stored service id")
		}
		var value any
		var err error
		if public {
			var profile providerServiceFields
			profile, err = row.profile(*id)
			value = providerServiceProfile{providerServiceFields: profile, Provider: provider}
		} else {
			value, err = row.definition(*id)
		}
		if err != nil {
			_ = iter.Close()
			return nil, err
		}
		result = append(result, value)
	}
	return result, userStorageError(iter.Close())
}

func (s serviceDeletionStore) disable(ctx context.Context, provider, id gocql.UUID) error {
	batch := s.cassandra.NewBatch(gocql.LoggedBatch).WithContext(ctx)
	batch.SetConsistency(gocql.LocalQuorum)
	batch.RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5})
	batch.Query(`UPDATE service SET enabled=? WHERE provider=? AND id=?`, false, provider, id)
	return userStorageError(s.cassandra.ExecuteBatch(batch))
}
