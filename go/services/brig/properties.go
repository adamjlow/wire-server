// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/apache/cassandra-gocql-driver/v2"
)

var (
	errPropertyKeyLarge      = errors.New("property key too large")
	errPropertyValueLarge    = errors.New("property value too large")
	errPropertyCount         = errors.New("too many properties")
	errPropertyInvalid       = errors.New("invalid property JSON")
	errPropertyStoredInvalid = errors.New("invalid stored property JSON")
)

type propertyService struct {
	store            propertyPersistence
	maxKey, maxValue int64
	notify           func(context.Context, []byte, string)
}

func newPropertyService(c Config, store propertyPersistence, notify func(context.Context, []byte, string)) *propertyService {
	s := &propertyService{store: store, maxKey: 1024, maxValue: 524288, notify: notify}
	if c.Settings.PropertyMaxKeyLen != nil {
		s.maxKey = *c.Settings.PropertyMaxKeyLen
	}
	if c.Settings.PropertyMaxValueLen != nil {
		s.maxValue = *c.Settings.PropertyMaxValueLen
	}
	return s
}

func validPropertyKey(key string) bool {
	for i := 0; i < len(key); i++ {
		if key[i] < 0x20 || key[i] > 0x7e {
			return false
		}
	}
	return true
}

func parsePropertyValue(raw []byte) (any, error) {
	if !validRegistrationJSONText(raw) {
		return nil, errPropertyInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	value, err := decodePropertyValue(decoder, 0)
	if err != nil {
		return nil, err
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return nil, errPropertyInvalid
	}
	return value, nil
}

type propertyJSONError struct{ message string }

func (e propertyJSONError) Error() string        { return e.message }
func (e propertyJSONError) Is(target error) bool { return target == errPropertyInvalid }

// The pinned native Aeson parser keeps the first occurrence of duplicate keys.
// Use tokens rather than Go's object decoder, which overwrites earlier keys.
func decodePropertyValue(decoder *json.Decoder, depth int) (any, error) {
	if depth > 10000 {
		return nil, errPropertyInvalid
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, errPropertyInvalid
	}
	delimiter, compound := token.(json.Delim)
	if !compound {
		return token, nil
	}
	switch delimiter {
	case '{':
		object := map[string]any{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err == io.EOF {
				return nil, propertyJSONError{"Unexpected end-of-input, expecting record key literal or }"}
			}
			key, ok := keyToken.(string)
			if err != nil || !ok {
				return nil, errPropertyInvalid
			}
			value, err := decodePropertyValue(decoder, depth+1)
			if err != nil {
				return nil, err
			}
			if _, exists := object[key]; !exists {
				object[key] = value
			}
		}
		end, err := decoder.Token()
		if err == io.EOF {
			return nil, propertyJSONError{"Unexpected end-of-input, expecting record key literal or }"}
		}
		if err != nil || end != json.Delim('}') {
			return nil, errPropertyInvalid
		}
		return object, nil
	case '[':
		array := []any{}
		for decoder.More() {
			value, err := decodePropertyValue(decoder, depth+1)
			if err != nil {
				return nil, err
			}
			array = append(array, value)
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim(']') {
			return nil, errPropertyInvalid
		}
		return array, nil
	default:
		return nil, errPropertyInvalid
	}
}

func (s propertyService) set(ctx context.Context, user gocql.UUID, key string, raw []byte, connection, requestID string) error {
	if int64(len(key)) > s.maxKey {
		return errPropertyKeyLarge
	}
	_, exists, err := s.store.lookup(ctx, user, key)
	if err != nil {
		return err
	}
	if !exists {
		count, err := s.store.count(ctx, user)
		if err != nil {
			return err
		}
		if count >= 16 {
			return errPropertyCount
		}
	}
	if int64(len(raw)) > s.maxValue {
		return errPropertyValueLarge
	}
	parsed, err := parsePropertyValue(raw)
	if err != nil {
		return err
	}
	if err := s.store.write(ctx, user, key, raw); err != nil {
		return err
	}
	s.event(ctx, user, connection, requestID, map[string]any{"type": "user.properties-set", "key": key, "value": parsed})
	return nil
}

func (s propertyService) event(ctx context.Context, user gocql.UUID, connection, requestID string, event map[string]any) {
	if s.notify == nil {
		return
	}
	body, err := json.Marshal([]any{map[string]any{
		"recipients": []any{map[string]any{"user_id": user.String(), "route": "direct", "clients": []string{}}},
		"origin":     user.String(), "origin_connection": connection, "payload": []any{event}, "is_cells_event": false,
	}})
	if err == nil {
		s.notify(ctx, body, requestID)
	}
}

func propertyHTTPError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errPropertyKeyLarge):
		wireError(w, 403, "property-key-too-large", "The property key is too large.")
	case errors.Is(err, errPropertyValueLarge):
		wireError(w, 403, "property-value-too-large", "The property value is too large")
	case errors.Is(err, errPropertyCount):
		wireError(w, 403, "too-many-properties", "Too many properties")
	case errors.Is(err, errPropertyInvalid):
		message := "Invalid JSON"
		var parseError propertyJSONError
		if errors.As(err, &parseError) {
			message = parseError.message
		}
		authenticationWireError(w, 400, "bad-request", message)
	default:
		wireError(w, 500, "internal-server-error", "Internal Server Error")
	}
}

func (s propertyService) handle(w http.ResponseWriter, r *http.Request, path string) bool {
	all := path == "/properties-values"
	escapedPath := r.URL.EscapedPath()
	if version := r.Header.Get("X-Wire-API-Version"); version != "" {
		escapedPath = strings.TrimPrefix(escapedPath, "/v"+version)
	}
	escapedKey := strings.TrimPrefix(escapedPath, "/properties/")
	keyPath := strings.HasPrefix(path, "/properties/") && escapedKey != "" && !strings.Contains(escapedKey, "/")
	if !all && path != "/properties" && !keyPath {
		return false
	}
	read := r.Method == http.MethodGet || r.Method == http.MethodHead
	write := r.Method == http.MethodPut && keyPath
	remove := r.Method == http.MethodDelete && !all
	if (!read && !write && !remove) || all && !read {
		w.WriteHeader(405)
		return true
	}
	user, ok := userHeader(w, r)
	if !ok {
		return true
	}
	connection := ""
	if !read {
		values, exists := r.Header["Z-Connection"]
		if !exists || len(values) == 0 {
			routeParseError(w, "Header Z-Connection is required")
			return true
		}
		connection = values[0]
	}
	key := strings.TrimPrefix(path, "/properties/")
	if keyPath && !validPropertyKey(key) {
		routeParseError(w, "Invalid printable ASCII characters")
		return true
	}
	ctx := r.Context()
	if read {
		if keyPath {
			raw, exists, err := s.store.lookup(ctx, user, key)
			if err != nil {
				propertyHTTPError(w, err)
				return true
			}
			if !exists {
				w.Header().Set("Content-Type", "text/plain;charset=utf-8")
				w.WriteHeader(404)
				return true
			}
			w.Header().Set("Content-Type", "application/json;charset=utf-8")
			w.WriteHeader(200)
			if r.Method != http.MethodHead {
				_, _ = w.Write(raw)
			}
		} else if all {
			values, err := s.store.all(ctx, user)
			if err != nil {
				propertyHTTPError(w, err)
				return true
			}
			parsed := make(map[string]any, len(values))
			for key, raw := range values {
				value, err := parsePropertyValue(raw)
				if err != nil {
					propertyHTTPError(w, errPropertyStoredInvalid)
					return true
				}
				parsed[key] = value
			}
			readJSON(w, r, parsed)
		} else {
			keys, err := s.store.keys(ctx, user)
			if err != nil {
				propertyHTTPError(w, err)
				return true
			}
			readJSON(w, r, keys)
		}
		return true
	}
	var err error
	if write {
		media, _, parseErr := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if parseErr != nil || !strings.EqualFold(media, "application/json") {
			w.WriteHeader(415)
			return true
		}
		// Only one byte past the configured boundary is needed for native error
		// precedence. Avoid buffering an arbitrarily large untrusted request.
		limit := s.maxValue
		if limit < 0 {
			limit = 0
		}
		if limit < int64(^uint64(0)>>1) {
			limit++
		}
		raw, readErr := io.ReadAll(io.LimitReader(r.Body, limit))
		if readErr != nil {
			propertyHTTPError(w, errPropertyInvalid)
			return true
		}
		err = s.set(ctx, user, key, raw, connection, r.Header.Get("Request-Id"))
	} else if keyPath {
		err = s.store.remove(ctx, user, key)
		if err == nil {
			s.event(ctx, user, connection, r.Header.Get("Request-Id"), map[string]any{"type": "user.properties-delete", "key": key})
		}
	} else {
		err = s.store.clear(ctx, user)
		if err == nil {
			s.event(ctx, user, connection, r.Header.Get("Request-Id"), map[string]any{"type": "user.properties-clear"})
		}
	}
	if err != nil {
		propertyHTTPError(w, err)
		return true
	}
	w.WriteHeader(200)
	return true
}
