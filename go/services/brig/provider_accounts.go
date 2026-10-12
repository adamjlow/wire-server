// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"errors"
	"unicode/utf8"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type providerAccount struct {
	ID          gocql.UUID `json:"id"`
	Name        string     `json:"name"`
	Email       string     `json:"email"`
	URL         string     `json:"url"`
	Description string     `json:"description"`
}

type providerProfileUpdate struct{ name, url, description *string }

func decodeProviderProfileUpdate(raw json.RawMessage) (providerProfileUpdate, error) {
	var update providerProfileUpdate
	fields, err := registrationObject(raw)
	if err != nil {
		return update, err
	}
	update.name, err = registrationText(fields, "name", false)
	if err != nil || update.name != nil && (utf8.RuneCountInString(*update.name) < 1 || utf8.RuneCountInString(*update.name) > 128) {
		return update, errors.New("invalid provider name")
	}
	update.url, err = registrationText(fields, "url", false)
	if err != nil {
		return update, err
	}
	if update.url != nil {
		canonical, err := storedHTTPSURL([]byte(*update.url))
		if err != nil {
			return update, err
		}
		text := string(canonical)
		update.url = &text
	}
	update.description, err = registrationText(fields, "description", false)
	return update, err
}

type providerAccountStore struct{ cassandra *gocql.Session }

type providerAccountData struct {
	name, url, description string
	email                  *string
}

func (s providerAccountStore) lookup(ctx context.Context, id gocql.UUID) (*providerAccount, error) {
	data, err := s.data(ctx, id)
	if err != nil || data == nil || data.email == nil {
		return nil, err
	}
	return &providerAccount{ID: id, Name: data.name, Email: *data.email, URL: data.url, Description: data.description}, nil
}

func (s providerAccountStore) data(ctx context.Context, id gocql.UUID) (*providerAccountData, error) {
	var name, email, description *string
	var rawURL []byte
	err := s.cassandra.Query(`SELECT name,email,url,descr FROM provider WHERE id=?`, id).
		WithContext(ctx).Consistency(gocql.LocalQuorum).
		RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).Scan(&name, &email, &rawURL, &description)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, userStorageError(err)
	}
	url, err := storedHTTPSURL(rawURL)
	if err != nil || name == nil || description == nil || !utf8.ValidString(*name) || !utf8.ValidString(*description) {
		return nil, errors.New("invalid stored provider")
	}
	if email != nil {
		canonicalEmail, err := canonicalRegistrationEmail(*email)
		if err != nil {
			return nil, errors.New("invalid stored provider email")
		}
		email = &canonicalEmail
	}
	return &providerAccountData{name: *name, email: email, url: string(url), description: *description}, nil
}

func (s providerAccountStore) update(ctx context.Context, id gocql.UUID, update providerProfileUpdate) error {
	batch := s.cassandra.NewBatch(gocql.UnloggedBatch).WithContext(ctx)
	batch.SetConsistency(gocql.LocalQuorum)
	batch.RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5})
	if update.name != nil {
		batch.Query(`UPDATE provider SET name=? WHERE id=?`, *update.name, id)
	}
	if update.url != nil {
		batch.Query(`UPDATE provider SET url=? WHERE id=?`, []byte(*update.url), id)
	}
	if update.description != nil {
		batch.Query(`UPDATE provider SET descr=? WHERE id=?`, *update.description, id)
	}
	if len(batch.Entries) == 0 {
		return nil
	}
	return userStorageError(s.cassandra.ExecuteBatch(batch))
}
