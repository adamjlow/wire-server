// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"unicode/utf8"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type providerNewService struct {
	name, summary, description, url string
	key                             []byte
	token                           *string
	assets                          []userAsset
	tags                            []int64
}

func (providerNewService) String() string { return "<ProviderNewService>" }

type providerNewServiceReply struct {
	ID    gocql.UUID `json:"id"`
	Token *string    `json:"auth_token,omitempty"`
}

func (providerNewServiceReply) String() string { return "<ProviderNewServiceReply>" }

func decodeProviderNewService(raw json.RawMessage) (providerNewService, error) {
	var result providerNewService
	fields, err := registrationObject(raw)
	if err != nil {
		return result, err
	}
	for _, field := range []struct {
		name    string
		into    *string
		maximum int
	}{{"name", &result.name, 128}, {"summary", &result.summary, 128}, {"description", &result.description, 1024}, {"base_url", &result.url, 0}} {
		value, err := registrationText(fields, field.name, true)
		if err != nil {
			return result, err
		}
		*field.into = *value
		if field.maximum != 0 {
			n := utf8.RuneCountInString(*value)
			if n < 1 || n > field.maximum {
				return result, errors.New("invalid service text")
			}
		}
	}
	url, err := storedHTTPSURL([]byte(result.url))
	if err != nil {
		return result, err
	}
	result.url = string(url)
	key, err := registrationText(fields, "public_key", true)
	if err != nil {
		return result, err
	}
	result.key, err = nativeServicePEM(*key)
	if err != nil {
		return result, err
	}
	result.token, err = registrationText(fields, "auth_token", false)
	if err != nil || result.token != nil && !validActivationValue(*result.token) {
		return result, errors.New("invalid service token")
	}
	if !registrationPresent(fields["assets"]) {
		return result, errors.New("service assets required")
	}
	result.assets, err = registrationAssets(fields["assets"])
	if err != nil {
		return result, err
	}
	var tags []string
	if json.Unmarshal(fields["tags"], &tags) != nil {
		return result, errors.New("invalid service tags")
	}
	seen := map[int64]bool{}
	for _, tag := range tags {
		found := false
		for index, name := range providerServiceTags {
			if tag == name {
				seen[int64(1)<<index] = true
				found = true
				break
			}
		}
		if !found {
			return result, errors.New("invalid service tag")
		}
	}
	if len(seen) < 1 || len(seen) > 3 {
		return result, errors.New("invalid service tag count")
	}
	for tag := range seen {
		result.tags = append(result.tags, tag)
	}
	sort.Slice(result.tags, func(i, j int) bool { return result.tags[i] < result.tags[j] })
	return result, nil
}

type providerServiceCreation struct {
	auth     providerAuthHTTP
	account  func(context.Context, gocql.UUID) (*providerAccount, error)
	key      func(context.Context, []byte) (int32, []byte, error)
	insert   func(context.Context, gocql.UUID, providerNewService, string, int32, []byte) (gocql.UUID, error)
	newToken func() (string, error)
}

func newServiceToken() (string, error) {
	var value [18]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", errors.New("service token generation failed")
	}
	return base64.URLEncoding.EncodeToString(value[:]), nil
}

func (s providerServiceCreation) create(ctx context.Context, provider gocql.UUID, request providerNewService) (providerNewServiceReply, error) {
	var reply providerNewServiceReply
	if s.account == nil || s.key == nil || s.insert == nil {
		return reply, errors.New("service creation unavailable")
	}
	account, err := s.account(ctx, provider)
	if err != nil {
		return reply, err
	}
	if account == nil {
		return reply, errProviderDeletionInvalid
	}
	size, fingerprint, err := s.key(ctx, request.key)
	if err != nil {
		return reply, err
	}
	token := request.token
	if token == nil {
		if s.newToken == nil {
			return reply, errors.New("service token generation unavailable")
		}
		value, err := s.newToken()
		if err != nil {
			return reply, err
		}
		token = &value
		reply.Token = token
	}
	reply.ID, err = s.insert(ctx, provider, request, *token, size, fingerprint)
	return reply, err
}

func (s serviceDeletionStore) create(ctx context.Context, provider gocql.UUID, request providerNewService, token string, size int32, fingerprint []byte) (gocql.UUID, error) {
	id, err := gocql.RandomUUID()
	if err != nil {
		return gocql.UUID{}, userStorageError(err)
	}
	keys := []map[string]any{{"typ": int32(0), "size": size, "pem": request.key}}
	err = s.cassandra.Query(`INSERT INTO service (provider,id,name,summary,descr,base_url,auth_tokens,pubkeys,fingerprints,assets,tags,enabled) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`, provider, id, request.name, request.summary, request.description, []byte(request.url), []string{token}, keys, [][]byte{fingerprint}, request.assets, request.tags, false).
		WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5}).Exec()
	return id, userStorageError(err)
}

func (s providerServiceCreation) handle(w http.ResponseWriter, r *http.Request) {
	provider, ok := providerHeader(w, r)
	if !ok {
		return
	}
	if !publicClientContentType(w, r) {
		return
	}
	raw, err := publicClientJSON(r)
	var request providerNewService
	if err == nil {
		request, err = decodeProviderNewService(raw)
	}
	if err != nil {
		var pemError servicePEMError
		if errors.As(err, &pemError) {
			wireError(w, 400, "bad-request", "Error in $['public_key']: Failed reading: "+pemError.Error())
		} else {
			authenticationWireError(w, 400, "bad-request", "Invalid new service request")
		}
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
	reply, err := s.create(r.Context(), provider, request)
	if errors.Is(err, errProviderDeletionInvalid) {
		authenticationWireError(w, 404, "not-found", "Provider not found.")
	} else if errors.Is(err, errServiceKeyInvalid) {
		authenticationWireError(w, 400, "invalid-service-key", "Invalid service key.")
	} else if err != nil {
		authenticationHTTPError(w, err)
	} else {
		w.Header().Set("Content-Type", "application/json;charset=utf-8")
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(reply)
	}
}
