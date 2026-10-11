// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/apache/cassandra-gocql-driver/v2"
	"golang.org/x/sync/errgroup"
)

type clientBulkReads struct {
	clients func(context.Context, gocql.UUID) ([]storedClient, error)
	ids     func(context.Context, gocql.UUID) ([]string, error)
}

func decodeClientUsers(reader io.Reader) ([]gocql.UUID, error) {
	failure := errors.New("invalid client user set")
	decoder := json.NewDecoder(reader)
	var raw json.RawMessage
	if decoder.Decode(&raw) != nil || !validRegistrationJSONText(raw) {
		return nil, failure
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return nil, failure
	}
	obj, err := registrationObject(raw)
	if err != nil || !registrationPresent(obj["users"]) {
		return nil, failure
	}
	var values []json.RawMessage
	if json.Unmarshal(obj["users"], &values) != nil {
		return nil, failure
	}
	seen := map[string]gocql.UUID{}
	for _, value := range values {
		var text string
		if json.Unmarshal(value, &text) != nil {
			return nil, failure
		}
		canonical, valid := tokenUUID(text)
		if !valid {
			return nil, failure
		}
		id, _ := gocql.ParseUUID(canonical)
		seen[canonical] = id
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	result := make([]gocql.UUID, 0, len(keys))
	for _, key := range keys {
		result = append(result, seen[key])
	}
	return result, nil
}

func nativeClientIDSet(values []string) ([]string, error) {
	words := make([]uint64, 0, len(values))
	for _, value := range values {
		canonical, err := parseClientID(value)
		if err != nil {
			return nil, err
		}
		word, _ := strconv.ParseUint(canonical, 16, 64)
		words = append(words, word)
	}
	slices.Sort(words)
	words = slices.Compact(words)
	result := make([]string, 0, len(words))
	for _, word := range words {
		result = append(result, strconv.FormatUint(word, 16))
	}
	return result, nil
}

func (s clientBulkReads) lookup(ctx context.Context, users []gocql.UUID, full bool) (map[string]any, error) {
	if full && s.clients == nil || !full && s.ids == nil {
		return nil, errors.New("bulk client storage unavailable")
	}
	result := map[string]any{}
	var lock sync.Mutex
	group, workCtx := errgroup.WithContext(ctx)
	limit := 16
	if full {
		limit = 50
	}
	group.SetLimit(limit)
	for _, user := range users {
		if workCtx.Err() != nil {
			break
		}
		group.Go(func() error {
			var value any
			if full {
				clients, err := s.clients(workCtx, user)
				if err != nil {
					return err
				}
				clients, err = nativeClientSet(clients)
				if err != nil {
					return err
				}
				objects := make([]map[string]any, 0, len(clients))
				for _, client := range clients {
					// Internal API uses Client's latest unversioned schema, even
					// when the public development API version is disabled.
					object, err := client.wire(19)
					if err != nil {
						return err
					}
					objects = append(objects, object)
				}
				value = objects
			} else {
				ids, err := s.ids(workCtx, user)
				if err != nil {
					return err
				}
				ids, err = nativeClientIDSet(ids)
				if err != nil {
					return err
				}
				value = ids
			}
			lock.Lock()
			result[user.String()] = value
			lock.Unlock()
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}
	// Cancellation before scheduling must not look like a partial successful map.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (s clientBulkReads) handle(w http.ResponseWriter, r *http.Request, full bool) {
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(media, "application/json") {
		w.WriteHeader(415)
		return
	}
	users, err := decodeClientUsers(r.Body)
	if err != nil {
		wireError(w, 400, "bad-request", "Invalid client user set")
		return
	}
	result, err := s.lookup(r.Context(), users, full)
	if err != nil {
		authenticationWireError(w, 500, "server-error", "Internal Server Error")
		return
	}
	readJSON(w, r, result)
}
