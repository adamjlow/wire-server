// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/apache/cassandra-gocql-driver/v2"
)

var errProviderDeletionInvalid = errors.New("provider does not exist")

type providerDeletionService struct {
	auth          providerAuthHTTP
	account       func(context.Context, gocql.UUID) (*providerAccount, error)
	services      func(context.Context, gocql.UUID) ([]providerDeletionServiceRecord, error)
	disconnect    func(context.Context, gocql.UUID, gocql.UUID) error
	remove        func(context.Context, gocql.UUID, gocql.UUID, deletionServiceRecord) error
	removeKey     func(context.Context, string) error
	removeAccount func(context.Context, gocql.UUID) error
}

type providerDeletionServiceRecord struct {
	id     gocql.UUID
	record deletionServiceRecord
}

func (s providerDeletionService) execute(ctx context.Context, id gocql.UUID, password string) error {
	if s.account == nil {
		return errors.New("provider deletion lookup unavailable")
	}
	account, err := s.account(ctx, id)
	if err != nil {
		return err
	}
	if account == nil {
		return errProviderDeletionInvalid
	}
	valid, err := s.auth.check(ctx, id, password)
	if err != nil {
		return err
	}
	if !valid {
		return authInvalidCredentials
	}
	if s.services == nil || s.disconnect == nil || s.remove == nil || s.removeKey == nil || s.removeAccount == nil {
		return errors.New("provider deletion mutation unavailable")
	}
	services, err := s.services(ctx, id)
	if err != nil {
		return err
	}
	for _, service := range services {
		if err := s.disconnect(ctx, id, service.id); err != nil {
			return err
		}
		if err := s.remove(ctx, id, service.id, service.record); err != nil {
			return err
		}
	}
	if err := s.removeKey(ctx, account.Email); err != nil {
		return err
	}
	return s.removeAccount(ctx, id)
}

func (s serviceDeletionStore) providerServices(ctx context.Context, provider gocql.UUID) ([]providerDeletionServiceRecord, error) {
	iter := s.cassandra.Query(`SELECT id,name,summary,descr,base_url,auth_tokens,pubkeys,assets,tags,enabled FROM service WHERE provider=?`, provider).
		WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).PageSize(0).Iter()
	var result []providerDeletionServiceRecord
	var id *gocql.UUID
	var row deletionServiceRow
	for iter.Scan(&id, &row.name, &row.summary, &row.description, &row.baseURL, &row.tokens, &row.keys, &row.assets, &row.tags, &row.enabled) {
		if id == nil {
			_ = iter.Close()
			return nil, errors.New("invalid stored provider service")
		}
		if err := row.validate(); err != nil {
			_ = iter.Close()
			return nil, err
		}
		result = append(result, providerDeletionServiceRecord{id: *id, record: deletionServiceRecord{name: *row.name, tags: append([]int64(nil), row.tags...)}})
	}
	if err := iter.Close(); err != nil {
		return nil, userStorageError(err)
	}
	return result, nil
}

func (s providerAccountStore) deleteKey(ctx context.Context, email string) error {
	key, err := emailUniqueKey(email)
	if err != nil {
		return err
	}
	return userStorageError(s.cassandra.Query(`DELETE FROM provider_keys WHERE key=?`, key).
		WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5}).Exec())
}

func (s providerAccountStore) delete(ctx context.Context, id gocql.UUID) error {
	return userStorageError(s.cassandra.Query(`DELETE FROM provider WHERE id=?`, id).
		WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5}).Exec())
}

func (s providerDeletionService) handle(w http.ResponseWriter, r *http.Request, path string) bool {
	if path != "/provider" || r.Method != http.MethodDelete {
		return false
	}
	id, valid := providerHeader(w, r)
	if !valid {
		return true
	}
	if !publicClientContentType(w, r) {
		return true
	}
	raw, err := publicClientJSON(r)
	var fields map[string]json.RawMessage
	var password *string
	if err == nil {
		fields, err = registrationObject(raw)
	}
	if err == nil {
		password, err = registrationText(fields, "password", true)
	}
	if err != nil || validatePlainPassword(*password, 6) != nil {
		authenticationWireError(w, 400, "bad-request", "Invalid provider deletion request")
		return true
	}
	if s.auth.secondFactorEnabled == nil {
		authenticationHTTPError(w, errors.New("provider feature lookup unavailable"))
		return true
	}
	enabled, err := s.auth.secondFactorEnabled(r.Context(), r.Header.Get("Request-Id"))
	if err != nil {
		authenticationHTTPError(w, err)
		return true
	}
	if enabled {
		authenticationWireError(w, 403, "access-denied", "Access denied.")
		return true
	}
	err = s.execute(r.Context(), id, *password)
	if errors.Is(err, errProviderDeletionInvalid) {
		authenticationWireError(w, 404, "not-found", "Provider not found.")
	} else if err != nil {
		authenticationHTTPError(w, err)
	} else {
		w.WriteHeader(200)
	}
	return true
}
