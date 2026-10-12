// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type providerServiceUpdate struct {
	name, summary, description *string
	assets                     *[]userAsset
	tags                       *[]int64
}

func decodeProviderServiceUpdate(raw json.RawMessage) (providerServiceUpdate, error) {
	var result providerServiceUpdate
	fields, err := registrationObject(raw)
	if err != nil {
		return result, err
	}
	for _, field := range []struct {
		name    string
		into    **string
		maximum int
	}{{"name", &result.name, 128}, {"summary", &result.summary, 128}, {"description", &result.description, 1024}} {
		value, err := registrationText(fields, field.name, false)
		if err != nil {
			return result, err
		}
		*field.into = value
		if value != nil {
			n := utf8.RuneCountInString(*value)
			if n < 1 || n > field.maximum {
				return result, errors.New("invalid service update text")
			}
		}
	}
	if registrationPresent(fields["assets"]) {
		assets, err := registrationAssets(fields["assets"])
		if err != nil {
			return result, err
		}
		result.assets = &assets
	}
	if registrationPresent(fields["tags"]) {
		var values []string
		if json.Unmarshal(fields["tags"], &values) != nil {
			return result, errors.New("invalid service tags")
		}
		set := map[int64]bool{}
		for _, value := range values {
			found := false
			for index, name := range providerServiceTags {
				if name == value {
					set[int64(1)<<index] = true
					found = true
					break
				}
			}
			if !found {
				return result, errors.New("invalid service tag")
			}
		}
		if len(set) < 1 || len(set) > 3 {
			return result, errors.New("invalid service tag count")
		}
		tags := []int64{}
		for index := range providerServiceTags {
			tag := int64(1) << index
			if set[tag] {
				tags = append(tags, tag)
			}
		}
		result.tags = &tags
	}
	return result, nil
}

type serviceUpdateQuery struct {
	statement string
	values    []any
}

func serviceUpdateQueries(provider, id gocql.UUID, old providerServiceDefinition, update providerServiceUpdate) ([]serviceUpdateQuery, error) {
	queries := []serviceUpdateQuery{}
	add := func(statement string, values ...any) {
		queries = append(queries, serviceUpdateQuery{statement, values})
	}
	oldTags := []int64{}
	for _, name := range old.Tags {
		for index, tag := range providerServiceTags {
			if name == tag {
				oldTags = append(oldTags, int64(1)<<index)
				break
			}
		}
	}
	indexTags := func(oldName, newName string, oldTags, newTags []int64) error {
		before, err := deletionServiceTagMasks(oldTags)
		if err != nil {
			return err
		}
		after, err := deletionServiceTagMasks(newTags)
		if err != nil {
			return err
		}
		oldLower, newLower := deletionServiceLower(oldName), deletionServiceLower(newName)
		contains := func(values []int64, target int64) bool {
			for _, v := range values {
				if v == target {
					return true
				}
			}
			return false
		}
		for _, mask := range before {
			if oldLower != newLower || !contains(after, mask) {
				add(`DELETE FROM service_tag WHERE bucket=? AND tag=? AND name=? AND service=?`, int32(201608), mask, oldLower, id)
			}
		}
		for _, mask := range after {
			if oldLower != newLower || !contains(before, mask) {
				add(`INSERT INTO service_tag (bucket,tag,name,service,provider) VALUES (?,?,?,?,?)`, int32(201608), mask, newLower, id, provider)
			}
		}
		return nil
	}
	name := old.Name
	if update.name != nil {
		name = *update.name
		add(`UPDATE service SET name=? WHERE provider=? AND id=?`, name, provider, id)
		if old.Enabled {
			prefix := func(value string) string {
				ch, _ := utf8.DecodeRuneInString(value)
				return deletionServiceLower(string(ch))
			}
			add(`DELETE FROM service_prefix WHERE prefix=? AND name=? AND service=?`, prefix(old.Name), deletionServiceLower(old.Name), id)
			add(`INSERT INTO service_prefix (prefix,name,service,provider) VALUES (?,?,?,?)`, prefix(name), deletionServiceLower(name), id, provider)
			if err := indexTags(old.Name, name, oldTags, oldTags); err != nil {
				return nil, err
			}
		}
	}
	if update.tags != nil {
		add(`UPDATE service SET tags=? WHERE provider=? AND id=?`, *update.tags, provider, id)
		if old.Enabled {
			if err := indexTags(old.Name, name, oldTags, *update.tags); err != nil {
				return nil, err
			}
		}
	}
	if update.summary != nil {
		add(`UPDATE service SET summary=? WHERE provider=? AND id=?`, *update.summary, provider, id)
	}
	if update.description != nil {
		add(`UPDATE service SET descr=? WHERE provider=? AND id=?`, *update.description, provider, id)
	}
	if update.assets != nil {
		add(`UPDATE service SET assets=? WHERE provider=? AND id=?`, *update.assets, provider, id)
	}
	return queries, nil
}

func (s serviceDeletionStore) updateProfile(ctx context.Context, provider, id gocql.UUID, old providerServiceDefinition, update providerServiceUpdate) error {
	queries, err := serviceUpdateQueries(provider, id, old, update)
	if err != nil {
		return err
	}
	if len(queries) == 0 {
		return nil
	}
	batch := s.cassandra.NewBatch(gocql.UnloggedBatch).WithContext(ctx)
	batch.SetConsistency(gocql.LocalQuorum)
	batch.RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5})
	for _, query := range queries {
		batch.Query(query.statement, query.values...)
	}
	return userStorageError(s.cassandra.ExecuteBatch(batch))
}

type providerServiceProfileUpdate struct {
	auth    providerAuthHTTP
	account func(context.Context, gocql.UUID) (*providerAccount, error)
	get     func(context.Context, gocql.UUID, gocql.UUID, bool) (any, error)
	write   func(context.Context, gocql.UUID, gocql.UUID, providerServiceDefinition, providerServiceUpdate) error
}

func (s providerServiceProfileUpdate) update(ctx context.Context, provider, id gocql.UUID, request providerServiceUpdate) error {
	if s.account == nil || s.get == nil || s.write == nil {
		return errors.New("service update unavailable")
	}
	account, err := s.account(ctx, provider)
	if err != nil {
		return err
	}
	if account == nil {
		return errProviderDeletionInvalid
	}
	value, err := s.get(ctx, provider, id, false)
	if err != nil {
		return err
	}
	if value == nil {
		return errProviderServiceNotFound
	}
	old, ok := value.(providerServiceDefinition)
	if !ok {
		return errors.New("invalid service update definition")
	}
	return s.write(ctx, provider, id, old, request)
}

func serviceUpdateAccept(r *http.Request) bool {
	value := strings.Join(r.Header.Values("Accept"), ",")
	if value == "" {
		return true
	}
	for _, part := range strings.Split(value, ",") {
		media, parameters, err := mime.ParseMediaType(strings.TrimSpace(part))
		if err != nil {
			continue
		}
		if q, present := parameters["q"]; present {
			weight, err := strconv.ParseFloat(q, 64)
			if err != nil || weight <= 0 {
				continue
			}
		}
		if media == "*/*" || media == "text/*" || media == "application/*" || media == "text/plain" || media == "application/json" {
			return true
		}
	}
	return false
}

func (s providerServiceProfileUpdate) handle(w http.ResponseWriter, r *http.Request, provider, id gocql.UUID) {
	if !publicClientContentType(w, r) {
		return
	}
	if !serviceUpdateAccept(r) {
		w.WriteHeader(406)
		return
	}
	raw, err := publicClientJSON(r)
	var request providerServiceUpdate
	if err == nil {
		request, err = decodeProviderServiceUpdate(raw)
	}
	if err != nil {
		authenticationWireError(w, 400, "bad-request", "Invalid service update")
		return
	}
	if s.auth.secondFactorEnabled == nil {
		authenticationHTTPError(w, errors.New("service feature lookup unavailable"))
		return
	}
	enabled, err := s.auth.secondFactorEnabled(r.Context(), r.Header.Get("Request-Id"))
	if err != nil {
		authenticationHTTPError(w, err)
		return
	}
	if enabled {
		authenticationWireError(w, 403, "access-denied", "Access denied.")
		return
	}
	err = s.update(r.Context(), provider, id, request)
	if errors.Is(err, errProviderDeletionInvalid) {
		authenticationWireError(w, 404, "not-found", "Provider not found.")
	} else if errors.Is(err, errProviderServiceNotFound) {
		authenticationWireError(w, 404, "not-found", "Service not found.")
	} else if err != nil {
		authenticationHTTPError(w, err)
	} else {
		w.WriteHeader(200)
	}
}
