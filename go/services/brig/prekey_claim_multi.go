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

type userClientPrekeys map[string]map[string]*uncheckedPrekey

func decodePrekeyUserClients(raw json.RawMessage) (map[string][]string, error) {
	fields, err := registrationObject(raw)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	slices.Sort(names)
	result := make(map[string][]string, len(fields))
	for _, name := range names {
		user, valid := tokenUUID(name)
		if !valid {
			return nil, errors.New("invalid prekey user")
		}
		var ids []string
		if string(fields[name]) == "null" || json.Unmarshal(fields[name], &ids) != nil {
			return nil, errors.New("invalid prekey client set")
		}
		normalized, err := nativeClientIDSet(ids)
		if err != nil {
			return nil, err
		}
		result[user] = normalized
	}
	return result, nil
}

func decodePrekeyClients(reader io.Reader, qualified bool) (map[string]map[string][]string, error) {
	decoder := json.NewDecoder(reader)
	var raw json.RawMessage
	var trailing any
	if decoder.Decode(&raw) != nil || decoder.Decode(&trailing) != io.EOF {
		return nil, errors.New("invalid prekey map")
	}
	parsed, err := parsePropertyValue(raw)
	if err != nil {
		return nil, err
	}
	canonical, err := json.Marshal(parsed)
	if err != nil {
		return nil, err
	}
	if !qualified {
		users, err := decodePrekeyUserClients(canonical)
		return map[string]map[string][]string{"": users}, err
	}
	fields, err := registrationObject(canonical)
	if err != nil {
		return nil, err
	}
	domains := make([]string, 0, len(fields))
	for domain := range fields {
		domains = append(domains, domain)
	}
	slices.Sort(domains)
	result := make(map[string]map[string][]string, len(fields))
	for _, name := range domains {
		domain, err := parseEmailDomain(name)
		if err != nil {
			return nil, err
		}
		users, err := decodePrekeyUserClients(fields[name])
		if err != nil {
			return nil, err
		}
		result[domain] = users
	}
	return result, nil
}

func (s prekeyClaimService) multiLocal(ctx context.Context, protectee gocql.UUID, users map[string][]string, requestID string) (userClientPrekeys, error) {
	if err := s.guard(ctx, protectee, users, requestID); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(users))
	for user := range users {
		names = append(names, user)
	}
	slices.Sort(names)
	result := make(userClientPrekeys, len(users))
	var lock sync.Mutex
	for start := 0; start < len(names); start += 16 {
		end := min(start+16, len(names))
		group, workCtx := errgroup.WithContext(ctx)
		group.SetLimit(16)
		for _, name := range names[start:end] {
			group.Go(func() error {
				user, _ := gocql.ParseUUID(name)
				keys := make(map[string]*uncheckedPrekey, len(users[name]))
				ids, err := nativeClientIDSet(users[name])
				if err != nil {
					return err
				}
				for _, id := range ids {
					if err := workCtx.Err(); err != nil {
						return err
					}
					// The group guard has already approved all clients. Per-user claims
					// remain sequential in numeric ClientId order, with native noPrekeys.
					key, err := s.claimOrClean(workCtx, user, id)
					if err != nil {
						return err
					}
					keys[id] = key
				}
				lock.Lock()
				result[name] = keys
				lock.Unlock()
				return nil
			})
		}
		if err := group.Wait(); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (s prekeyClaimService) handleMulti(w http.ResponseWriter, r *http.Request, path string) bool {
	qualified := path == "/users/list-prekeys"
	if !qualified && path != "/users/prekeys" {
		return false
	}
	version, _ := strconv.Atoi(r.Header.Get("X-Wire-API-Version"))
	if !qualified && version >= 2 {
		return false
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return true
	}
	protectee, ok := userHeader(w, r)
	if !ok {
		return true
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(media, "application/json") {
		w.WriteHeader(415)
		return true
	}
	domains, err := decodePrekeyClients(r.Body, qualified)
	if err != nil {
		routeParseError(w, "Invalid prekey client map")
		return true
	}
	count := 0
	for _, users := range domains {
		count += len(users)
	}
	if count > s.maxUsers {
		authenticationWireError(w, 403, "too-many-clients", "Too many clients")
		return true
	}
	for domain := range domains {
		if domain != "" && domain != s.domain {
			return false
		}
	}
	result := map[string]userClientPrekeys{}
	// Empty qualified request has no local group and therefore no guard call;
	// the legacy empty map still invokes claimLocalMultiPrekeyBundles.
	for domain, users := range domains {
		keys, err := s.multiLocal(r.Context(), protectee, users, r.Header.Get("Request-Id"))
		if err != nil {
			prekeyClaimHTTPError(w, err)
			return true
		}
		result[domain] = keys
	}
	if !qualified {
		readJSON(w, r, result[""])
		return true
	}
	if version < 4 {
		readJSON(w, r, result)
		return true
	}
	readJSON(w, r, map[string]any{"qualified_user_client_prekeys": result})
	return true
}
