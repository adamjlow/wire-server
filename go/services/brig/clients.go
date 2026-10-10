// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func parseClientID(value string) (string, error) {
	if value == "" {
		return "", errors.New("invalid client ID")
	}
	for _, ch := range value {
		if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f' || ch >= 'A' && ch <= 'F') {
			return "", errors.New("invalid client ID")
		}
	}
	// Native's Integer hex parser permits arbitrarily many leading zeroes,
	// followed by a Word64 bound. ParseUint otherwise rejects >16 digits.
	first := 0
	for first < len(value)-1 && value[first] == '0' {
		first++
	}
	id, err := strconv.ParseUint(value[first:], 16, 64)
	if err != nil {
		return "", errors.New("invalid client ID")
	}
	return strconv.FormatUint(id, 16), nil
}

type storedClient struct {
	ID                   string
	Type                 int32
	Created              time.Time
	Label, Cookie, Model *string
	Class                *int32
	Capabilities         []int32
	PublicKeys           map[string][]byte
	LastActive           *time.Time
}

func (storedClient) String() string { return "<StoredClient>" }

func validSignatureScheme(value string) bool {
	return value == "ed25519" || value == "ecdsa_secp256r1_sha256" || value == "ecdsa_secp384r1_sha384" || value == "ecdsa_secp521r1_sha512"
}

func (c storedClient) validate() error {
	if _, err := parseClientID(c.ID); err != nil || c.Type < 0 || c.Type > 2 || c.Class != nil && (*c.Class < 0 || *c.Class > 3) {
		return errors.New("invalid stored client")
	}
	for _, capability := range c.Capabilities {
		if capability != 1 && capability != 2 {
			return errors.New("invalid stored client capability")
		}
	}
	for scheme := range c.PublicKeys {
		if !validSignatureScheme(scheme) {
			return errors.New("invalid stored client signature scheme")
		}
	}
	return nil
}

type clientStore struct{ cassandra *gocql.Session }

// Native fetches public keys before the client row; malformed orphan key rows
// must not disappear behind a successful missing-client result.
func (s clientStore) publicKeys(ctx context.Context, user gocql.UUID, client *string) (map[string]map[string][]byte, error) {
	query := `SELECT client,sig_scheme,key FROM mls_public_keys WHERE user=?`
	args := []any{user}
	if client != nil {
		query = `SELECT client,sig_scheme,key FROM mls_public_keys WHERE user=? AND client=?`
		args = append(args, *client)
	}
	iter := s.cassandra.Query(query, args...).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).Iter()
	result := map[string]map[string][]byte{}
	var rawID, scheme *string
	var key *[]byte
	for iter.Scan(&rawID, &scheme, &key) {
		if rawID == nil || scheme == nil || key == nil || !validSignatureScheme(*scheme) {
			_ = iter.Close()
			return nil, errors.New("invalid stored client public key")
		}
		id, err := parseClientID(*rawID)
		if err != nil {
			_ = iter.Close()
			return nil, errors.New("invalid stored client public key")
		}
		if result[id] == nil {
			result[id] = map[string][]byte{}
		}
		// The list native Map.fromListWith merge retains the first clustered
		// row for legacy client-text aliases with identical normalized IDs.
		if _, exists := result[id][*scheme]; !exists {
			result[id][*scheme] = append([]byte{}, (*key)...)
		}
	}
	if err := iter.Close(); err != nil {
		return nil, errors.New("client public key lookup failed")
	}
	return result, nil
}

func (s clientStore) read(ctx context.Context, user gocql.UUID, client *string) ([]storedClient, error) {
	keys, err := s.publicKeys(ctx, user, client)
	if err != nil {
		return nil, err
	}
	query := `SELECT client,type,tstamp,label,class,cookie,model,capabilities,last_active FROM clients WHERE user=?`
	args := []any{user}
	if client != nil {
		query += ` AND client=?`
		args = append(args, *client)
	}
	iter := s.cassandra.Query(query, args...).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).Iter()
	result := []storedClient{}
	var rawID *string
	var typ *int32
	var created *time.Time
	var c storedClient
	for iter.Scan(&rawID, &typ, &created, &c.Label, &c.Class, &c.Cookie, &c.Model, &c.Capabilities, &c.LastActive) {
		if rawID == nil || typ == nil || created == nil {
			_ = iter.Close()
			return nil, errors.New("invalid stored client")
		}
		id, err := parseClientID(*rawID)
		if err != nil {
			_ = iter.Close()
			return nil, errors.New("invalid stored client")
		}
		c.ID, c.Type, c.Created, c.PublicKeys = id, *typ, *created, keys[id]
		if c.PublicKeys == nil {
			c.PublicKeys = map[string][]byte{}
		}
		if err := c.validate(); err != nil {
			_ = iter.Close()
			return nil, err
		}
		result = append(result, c)
		// gocql can reuse slice backing storage across iterations.
		c = storedClient{}
	}
	if err := iter.Close(); err != nil {
		return nil, errors.New("client lookup failed")
	}
	return result, nil
}

func (s clientStore) get(ctx context.Context, user gocql.UUID, id string) (*storedClient, error) {
	canonical, err := parseClientID(id)
	if err != nil {
		return nil, err
	}
	rows, err := s.read(ctx, user, &canonical)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return &rows[0], nil
}

func (s clientStore) list(ctx context.Context, user gocql.UUID) ([]storedClient, error) {
	return s.read(ctx, user, nil)
}

func (s clientStore) prekeyIDs(ctx context.Context, user gocql.UUID, id string) ([]uint16, error) {
	canonical, err := parseClientID(id)
	if err != nil {
		return nil, err
	}
	iter := s.cassandra.Query(`SELECT key FROM prekeys WHERE user=? AND client=?`, user, canonical).
		WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).Iter()
	result := []uint16{}
	var key *int32
	for iter.Scan(&key) {
		if key == nil {
			_ = iter.Close()
			return nil, errors.New("invalid stored prekey ID")
		}
		result = append(result, uint16(*key))
	}
	if err := iter.Close(); err != nil {
		return nil, errors.New("prekey ID lookup failed")
	}
	return result, nil
}

func clientCapabilities(capabilities []int32, version int) any {
	values := []string{}
	for _, key := range []int32{1, 2} {
		present := false
		for _, stored := range capabilities {
			if stored == key {
				present = true
				break
			}
		}
		if present && (version >= 8 || key == 1) {
			if key == 1 {
				values = append(values, "legalhold-implicit-consent")
			} else {
				values = append(values, "consumable-notifications")
			}
		}
	}
	if version <= 6 {
		return map[string]any{"capabilities": values}
	}
	return values
}

func (c storedClient) wire(version int) (map[string]any, error) {
	if err := c.validate(); err != nil {
		return nil, err
	}
	id, _ := parseClientID(c.ID)
	keys := map[string][]byte{}
	for scheme, bytes := range c.PublicKeys {
		keys[scheme] = append([]byte{}, bytes...)
	}
	result := map[string]any{"id": id, "type": []string{"temporary", "permanent", "legalhold"}[c.Type], "time": c.Created.UTC().Format("2006-01-02T15:04:05.000Z"), "capabilities": clientCapabilities(c.Capabilities, version), "mls_public_keys": keys}
	if c.Class != nil {
		result["class"] = []string{"phone", "tablet", "desktop", "legalhold"}[*c.Class]
	}
	if c.Label != nil {
		result["label"] = *c.Label
	}
	if c.Cookie != nil {
		result["cookie"] = *c.Cookie
	}
	if c.Model != nil {
		result["model"] = *c.Model
	}
	if c.LastActive != nil {
		result["last_active"] = c.LastActive.UTC().Format(time.RFC3339Nano)
	}
	return result, nil
}
