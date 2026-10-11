// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type federationClientPrekey struct {
	Client string          `json:"client"`
	Prekey uncheckedPrekey `json:"prekey"`
}

func invalidFederationPrekey() error {
	return prekeyFederationFailure{code: 533, label: "federation-invalid-body", message: "Could not parse body"}
}
func decodeFederationPrekey(raw json.RawMessage) (*uncheckedPrekey, error) {
	fields, err := registrationObject(raw)
	if err != nil {
		return nil, err
	}
	id, err := federationInteger(fields["id"])
	if err != nil || id < 0 || id > 65535 {
		return nil, invalidFederationPrekey()
	}
	key, err := registrationText(fields, "key", true)
	if err != nil {
		return nil, err
	}
	return &uncheckedPrekey{ID: uint16(id), Key: *key}, nil
}
func decodeFederationClientPrekey(raw json.RawMessage) (*federationClientPrekey, error) {
	if string(raw) == "null" {
		return nil, nil
	}
	fields, err := registrationObject(raw)
	if err != nil {
		return nil, err
	}
	text, err := registrationText(fields, "client", true)
	if err != nil {
		return nil, err
	}
	client, err := parseClientID(*text)
	if err != nil {
		return nil, err
	}
	key, err := decodeFederationPrekey(fields["prekey"])
	if err != nil {
		return nil, err
	}
	return &federationClientPrekey{Client: client, Prekey: *key}, nil
}
func (s prekeyFederationClient) single(ctx context.Context, domain string, user gocql.UUID, id, requestID string) (*federationClientPrekey, error) {
	raw, err := s.call(ctx, domain, "claim-prekey", requestID, []string{user.String(), id})
	if err != nil {
		return nil, err
	}
	key, err := decodeFederationClientPrekey(raw)
	if err != nil {
		return nil, invalidFederationPrekey()
	}
	return key, nil
}
func (s prekeyFederationClient) bundle(ctx context.Context, domain string, user gocql.UUID, requestID string) (map[string]any, error) {
	raw, err := s.call(ctx, domain, "claim-prekey-bundle", requestID, user.String())
	if err != nil {
		return nil, err
	}
	fields, err := registrationObject(raw)
	if err != nil {
		return nil, invalidFederationPrekey()
	}
	text, err := registrationText(fields, "user", true)
	if err != nil {
		return nil, invalidFederationPrekey()
	}
	canonical, ok := tokenUUID(*text)
	if !ok {
		return nil, invalidFederationPrekey()
	}
	var clients []json.RawMessage
	if !registrationPresent(fields["clients"]) || json.Unmarshal(fields["clients"], &clients) != nil {
		return nil, invalidFederationPrekey()
	}
	result := []any{}
	for _, raw := range clients {
		key, err := decodeFederationClientPrekey(raw)
		if err != nil || key == nil {
			return nil, invalidFederationPrekey()
		}
		result = append(result, key)
	}
	return map[string]any{"user": canonical, "clients": result}, nil
}
func (s prekeyFederationClient) multi(ctx context.Context, domain string, users map[string][]string, requestID string) (userClientPrekeys, error) {
	raw, err := s.call(ctx, domain, "claim-multi-prekey-bundle", requestID, users)
	if err != nil {
		return nil, err
	}
	fields, err := registrationObject(raw)
	if err != nil {
		return nil, invalidFederationPrekey()
	}
	result := make(userClientPrekeys, len(fields))
	for text, raw := range fields {
		user, valid := tokenUUID(text)
		if !valid {
			return nil, invalidFederationPrekey()
		}
		clients, err := registrationObject(raw)
		if err != nil {
			return nil, invalidFederationPrekey()
		}
		keys := make(map[string]*uncheckedPrekey, len(clients))
		for name, raw := range clients {
			id, err := parseClientID(name)
			if err != nil {
				return nil, invalidFederationPrekey()
			}
			var key *uncheckedPrekey
			if string(raw) != "null" {
				key, err = decodeFederationPrekey(raw)
				if err != nil {
					return nil, invalidFederationPrekey()
				}
			}
			keys[id] = key
		}
		result[user] = keys
	}
	return result, nil
}
