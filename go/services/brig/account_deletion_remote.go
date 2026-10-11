// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/apache/cassandra-gocql-driver/v2"
	"golang.org/x/sync/errgroup"
)

type remoteAccountDeletion struct {
	statuses func(context.Context, string, gocql.UUID, []gocql.UUID) ([]gocql.UUID, error)
	remove   func(context.Context, string, gocql.UUID, []gocql.UUID) error
	notify   func(context.Context, []byte, string)
}

func (s connectionStore) remoteDeletionAccepted(ctx context.Context, domain string, deleted gocql.UUID, locals []gocql.UUID) ([]gocql.UUID, error) {
	accepted := make([]bool, len(locals))
	group, workerCtx := errgroup.WithContext(ctx)
	group.SetLimit(16)
	for i, user := range locals {
		group.Go(func() error {
			iter := s.cassandra.Query(`SELECT right_user,status FROM connection_remote WHERE left=? AND right_domain=? AND right_user IN ?`, user, domain, []gocql.UUID{deleted}).WithContext(workerCtx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).Iter()
			var target *gocql.UUID
			var relation *int32
			for iter.Scan(&target, &relation) {
				if target == nil || relation == nil || *relation < 0 || *relation > 11 {
					iter.Close()
					return errors.New("invalid remote deletion status")
				}
				if *relation == 0 {
					accepted[i] = true
				}
			}
			return userStorageError(iter.Close())
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}
	result := []gocql.UUID{}
	for i, user := range locals {
		if accepted[i] {
			result = append(result, user)
		}
	}
	return result, nil
}

func (s connectionStore) removeRemoteDeletedUser(ctx context.Context, domain string, deleted gocql.UUID, locals []gocql.UUID) error {
	group, workerCtx := errgroup.WithContext(ctx)
	group.SetLimit(16)
	for _, user := range locals {
		group.Go(func() error {
			return userStorageError(s.cassandra.Query(`DELETE FROM connection_remote WHERE left=? AND right_domain=? AND right_user=?`, user, domain, deleted).WithContext(workerCtx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 0}).Exec())
		})
	}
	return group.Wait()
}

func (s remoteAccountDeletion) execute(ctx context.Context, domain string, deleted gocql.UUID, locals []gocql.UUID, requestID string) error {
	if !deletionEventDomain(domain) || len(locals) < 1 || len(locals) > 1000 || s.statuses == nil || s.remove == nil || s.notify == nil {
		return errors.New("remote deletion unavailable")
	}
	accepted, err := s.statuses(ctx, domain, deleted, locals)
	if err != nil {
		return err
	}
	if err := (accountDeletionEvents{domain: domain, notify: s.notify}).direct(ctx, deleted, accepted, requestID); err != nil {
		return err
	}
	return s.remove(ctx, domain, deleted, locals)
}

func decodeRemoteAccountDeletion(raw []byte) (gocql.UUID, []gocql.UUID, error) {
	failure := errors.New("invalid remote deletion notification")
	var object map[string]json.RawMessage
	if !validRegistrationJSONText(raw) || json.Unmarshal(raw, &object) != nil || object == nil {
		return gocql.UUID{}, nil, failure
	}
	var user string
	var values []string
	if json.Unmarshal(object["user"], &user) != nil || json.Unmarshal(object["connections"], &values) != nil || len(values) < 1 || len(values) > 1000 {
		return gocql.UUID{}, nil, failure
	}
	id, err := registrationUUID(user)
	if err != nil {
		return gocql.UUID{}, nil, failure
	}
	locals := make([]gocql.UUID, len(values))
	for i, value := range values {
		locals[i], err = registrationUUID(value)
		if err != nil {
			return gocql.UUID{}, nil, failure
		}
	}
	return id, locals, nil
}

func (s remoteAccountDeletion) handle(w http.ResponseWriter, r *http.Request, path string) bool {
	if path != "/federation/on-user-deleted-connections" {
		return false
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return true
	}
	domain, err := parseEmailDomain(r.Header.Get("Wire-Origin-Domain"))
	if err != nil {
		routeParseError(w, "Invalid federation origin domain")
		return true
	}
	if !publicClientContentType(w, r) {
		return true
	}
	raw, err := publicClientJSON(r)
	if err != nil {
		routeParseError(w, "Invalid remote deletion notification")
		return true
	}
	user, locals, err := decodeRemoteAccountDeletion(raw)
	if err != nil {
		routeParseError(w, "Invalid remote deletion notification")
		return true
	}
	if err := s.execute(r.Context(), domain, user, locals, r.Header.Get("Request-Id")); err != nil {
		authenticationHTTPError(w, err)
	} else {
		readJSON(w, r, map[string]any{})
	}
	return true
}
