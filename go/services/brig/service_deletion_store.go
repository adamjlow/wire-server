// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/pem"
	"errors"
	"net/url"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/apache/cassandra-gocql-driver/v2"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

type serviceDeletionStore struct{ cassandra *gocql.Session }

type deletionServiceKey struct {
	Type *int32 `cql:"typ"`
	Size *int32 `cql:"size"`
	PEM  []byte `cql:"pem"`
}

type deletionServiceAsset struct {
	Type *int32  `cql:"typ"`
	Key  *string `cql:"key"`
	Size *int32  `cql:"size"`
}

type deletionServiceRow struct {
	name, summary, description *string
	baseURL                    []byte
	tokens                     []string
	keys                       []deletionServiceKey
	assets                     []deletionServiceAsset
	tags                       []int64
	enabled                    *bool
}

func (s serviceDeletionStore) lookup(ctx context.Context, provider, service gocql.UUID) (*deletionServiceRecord, error) {
	var row deletionServiceRow
	err := s.cassandra.Query(`SELECT name,summary,descr,base_url,auth_tokens,pubkeys,assets,tags,enabled FROM service WHERE provider=? AND id=?`, provider, service).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).Scan(&row.name, &row.summary, &row.description, &row.baseURL, &row.tokens, &row.keys, &row.assets, &row.tags, &row.enabled)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, userStorageError(err)
	}
	if err := row.validate(); err != nil {
		return nil, err
	}
	return &deletionServiceRecord{name: *row.name, tags: row.tags}, nil
}

func (r deletionServiceRow) validate() error {
	invalid := errors.New("invalid stored service")
	if r.name == nil || r.description == nil || r.enabled == nil || !utf8.ValidString(*r.name) || !utf8.ValidString(*r.description) || r.summary != nil && !utf8.ValidString(*r.summary) || len(r.tokens) == 0 || len(r.keys) == 0 {
		return invalid
	}
	// The native URL is an absolute strict URI whose scheme is exactly https.
	text := string(r.baseURL)
	if !strings.HasPrefix(text, "https:") {
		return invalid
	}
	for _, ch := range r.baseURL {
		if ch <= 32 || ch >= 127 {
			return invalid
		}
	}
	u, err := url.Parse(text)
	if err != nil || !u.IsAbs() {
		return invalid
	}
	for _, token := range r.tokens {
		if !utf8.ValidString(token) {
			return invalid
		}
	}
	for _, key := range r.keys {
		if key.Type == nil || *key.Type != 0 || key.Size == nil || key.PEM == nil {
			return invalid
		}
		block, rest := pem.Decode(key.PEM)
		if block == nil || len(strings.TrimSpace(string(rest))) != 0 {
			return invalid
		}
	}
	for _, asset := range r.assets {
		if asset.Type == nil || *asset.Type != 0 || asset.Key == nil || asset.Size != nil && *asset.Size != 0 && *asset.Size != 1 {
			return invalid
		}
		if _, valid := canonicalRegistrationAssetKey(*asset.Key); !valid {
			return invalid
		}
	}
	for _, tag := range r.tags {
		if tag <= 0 || tag > 1<<30 || tag&(tag-1) != 0 {
			return invalid
		}
	}
	return nil
}

// Native indexes contain every nonempty subset of up to three enum-bit tags.
func deletionServiceTagMasks(tags []int64) ([]int64, error) {
	tags = append([]int64(nil), tags...)
	sort.Slice(tags, func(i, j int) bool { return tags[i] < tags[j] })
	unique := tags[:0]
	for _, tag := range tags {
		if tag <= 0 || tag > 1<<30 || tag&(tag-1) != 0 {
			return nil, errors.New("invalid service tag")
		}
		if len(unique) == 0 || unique[len(unique)-1] != tag {
			unique = append(unique, tag)
		}
	}
	if len(unique) > 3 {
		return nil, errors.New("too many service tags")
	}
	var masks []int64
	for subset := 1; subset < 1<<len(unique); subset++ {
		var mask int64
		for i, tag := range unique {
			if subset&(1<<i) != 0 {
				mask |= tag
			}
		}
		masks = append(masks, mask)
	}
	return masks, nil
}

func (s serviceDeletionStore) remove(ctx context.Context, provider, service gocql.UUID, record deletionServiceRecord) error {
	iter := s.cassandra.Query(`SELECT team FROM service_whitelist_rev WHERE provider=? AND service=?`, provider, service).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5}).Iter()
	whitelist := s.cassandra.NewBatch(gocql.LoggedBatch).WithContext(ctx)
	whitelist.Cons = gocql.LocalQuorum
	whitelist.RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5})
	whitelist.Query(`DELETE FROM service_whitelist_rev WHERE provider=? AND service=?`, provider, service)
	var team *gocql.UUID
	for iter.Scan(&team) {
		if team == nil {
			_ = iter.Close()
			return errors.New("invalid service whitelist row")
		}
		whitelist.Query(`DELETE FROM service_whitelist WHERE team=? AND provider=? AND service=?`, *team, provider, service)
	}
	if err := iter.Close(); err != nil {
		return userStorageError(err)
	}
	if err := s.cassandra.ExecuteBatch(whitelist); err != nil {
		return userStorageError(err)
	}
	// Native unfolds tags only after whitelist cleanup has committed. A corrupt
	// overfull tag set must retain that phase and leave metadata for retry.
	masks, err := deletionServiceTagMasks(record.tags)
	if err != nil {
		return err
	}
	metadata := s.cassandra.NewBatch(gocql.UnloggedBatch).WithContext(ctx)
	metadata.Cons = gocql.LocalQuorum
	metadata.RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5})
	metadata.Query(`DELETE FROM service WHERE provider=? AND id=?`, provider, service)
	name := deletionServiceLower(record.name)
	first := ""
	if record.name != "" {
		ch, _ := utf8.DecodeRuneInString(record.name)
		first = deletionServiceLower(string(ch))
	}
	metadata.Query(`DELETE FROM service_prefix WHERE prefix=? AND name=? AND service=?`, first, name, service)
	for _, mask := range masks {
		metadata.Query(`DELETE FROM service_tag WHERE bucket=? AND tag=? AND name=? AND service=?`, int32(201608), mask, name, service)
	}
	return userStorageError(s.cassandra.ExecuteBatch(metadata))
}

// Data.Text lowercase expands dotted I and applies non-contextual mappings.
// Go's strings.ToLower loses that expansion; default locale casing changes
// final sigma. Construct a private caser because Caser is stateful.
func deletionServiceLower(value string) string {
	return cases.Lower(language.Und, cases.HandleFinalSigma(false)).String(value)
}
