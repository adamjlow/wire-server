// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/apache/cassandra-gocql-driver/v2"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

func decodeObject(r *http.Request) (map[string]json.RawMessage, error) {
	var value map[string]json.RawMessage
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&value); err != nil || value == nil {
		return nil, errors.New("invalid request body")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return nil, errors.New("invalid request body")
	}
	return value, nil
}
func tokenJSON(info tokenInfo, legacy bool) map[string]any {
	value := map[string]any{"team": info.Team.String(), "id": info.ID.String(), "created_at": info.CreatedAt, "description": info.Description}
	if info.IdP != nil {
		value["idp"] = info.IdP.String()
	}
	if !legacy {
		value["name"] = info.Name
	}
	return value
}
func serveTokens(w http.ResponseWriter, r *http.Request, c Config, store tokenStore, peers peerClient) bool {
	const base = "/scim/auth-tokens"
	if r.URL.Path != base && !strings.HasPrefix(r.URL.Path, base+"/") {
		return false
	}
	version, _ := strconv.Atoi(r.Header.Get("X-Wire-API-Version"))
	legacy := version < 8
	suffix := strings.TrimPrefix(r.URL.Path, base)
	if suffix != "" && (legacy || strings.Contains(suffix[1:], "/")) {
		w.WriteHeader(404)
		return true
	}
	method := r.Method
	if suffix == "" && method != http.MethodGet && method != http.MethodHead && method != http.MethodPost && method != http.MethodDelete || suffix != "" && method != http.MethodPut {
		w.WriteHeader(405)
		return true
	}
	fail := func(err error) bool { domainError(w, err); return true }
	bad := func() bool { return fail(&apiError{400, "bad-request", "Invalid request body"}) }
	var tokenID gocql.UUID
	var requested map[string]json.RawMessage
	var err error
	if method == http.MethodDelete {
		tokenID, err = gocql.ParseUUID(r.URL.Query().Get("id"))
		if err != nil {
			return bad()
		}
	}
	if method == http.MethodPut {
		tokenID, err = gocql.ParseUUID(suffix[1:])
		if err != nil {
			return bad()
		}
	}
	if method == http.MethodPost || method == http.MethodPut {
		requested, err = decodeObject(r)
		if err != nil {
			return bad()
		}
	}
	var description, name *string
	var password, code *string
	var idp *gocql.UUID
	if method == http.MethodPost {
		if json.Unmarshal(requested["description"], &description) != nil || description == nil {
			return bad()
		}
		if raw, present := requested["password"]; present {
			if json.Unmarshal(raw, &password) != nil {
				return bad()
			}
		}
		if password != nil && (utf8.RuneCountInString(*password) < 6 || utf8.RuneCountInString(*password) > 1024) {
			return bad()
		}
		if raw, present := requested["verification_code"]; present {
			if json.Unmarshal(raw, &code) != nil {
				return bad()
			}
		}
		if code != nil {
			if len(*code) < 6 || len(*code) > 20 {
				return bad()
			}
			for _, ch := range *code {
				if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_') {
					return bad()
				}
			}
		}
		if !legacy {
			if raw, present := requested["name"]; present {
				if json.Unmarshal(raw, &name) != nil {
					return bad()
				}
			}
			if raw, present := requested["idp"]; present && string(raw) != "null" {
				var encoded string
				if json.Unmarshal(raw, &encoded) != nil {
					return bad()
				}
				parsed, err := gocql.ParseUUID(encoded)
				if err != nil {
					return bad()
				}
				idp = &parsed
			}
		}
	}
	if method == http.MethodPut {
		if json.Unmarshal(requested["name"], &name) != nil || name == nil {
			return bad()
		}
	}
	user, requestID := r.Header.Get("Z-User"), r.Header.Get("Request-Id")
	team, err := peers.authorize(r.Context(), user, requestID)
	if err != nil {
		return fail(err)
	}
	switch method {
	case http.MethodGet, http.MethodHead:
		infos, err := store.teamTokens(r.Context(), team)
		if err != nil {
			return fail(err)
		}
		values := make([]map[string]any, len(infos))
		for i, info := range infos {
			values[i] = tokenJSON(info, legacy)
		}
		jsonResponse(w, r, map[string]any{"tokens": values})
	case http.MethodDelete:
		if err = store.deleteToken(r.Context(), team, tokenID); err != nil {
			return fail(err)
		}
		w.WriteHeader(http.StatusNoContent)
	case http.MethodPut:
		if err = store.renameToken(r.Context(), team, tokenID, *name); err != nil {
			return fail(err)
		}
		jsonResponse(w, r, []any{})
	case http.MethodPost:
		if err = peers.reauthenticate(r.Context(), user, requestID, password, code); err != nil {
			return fail(err)
		}
		tokens, err := store.teamTokens(r.Context(), team)
		if err != nil {
			return fail(err)
		}
		if c.MaxTokens == nil || len(tokens) >= *c.MaxTokens {
			return fail(&apiError{403, "token-limit-reached", "The limit of provisioning tokens per team has been reached"})
		}
		if legacy {
			ids, err := store.teamIdPs(r.Context(), team)
			if err != nil {
				return fail(err)
			}
			if len(ids) > 1 {
				return fail(&apiError{400, "more-than-one-idp", "Team can have at most one IdP configured: SCIM tokens can only be created for a team with at most one IdP"})
			}
			if len(ids) == 1 {
				idp = &ids[0]
			}
		} else if idp != nil {
			if err = store.requireIdP(r.Context(), *idp); err != nil {
				if errors.Is(err, errIdPNotFound) {
					return fail(&apiError{404, "not-found", "Could not find IdP."})
				}
				return fail(err)
			}
		}
		var secret [32]byte
		if _, err = rand.Read(secret[:]); err != nil {
			return fail(err)
		}
		id, err := gocql.RandomUUID()
		if err != nil {
			return fail(err)
		}
		info := tokenInfo{Team: team, ID: id, CreatedAt: time.Now().UTC(), IdP: idp, Description: *description, Name: id.String()}
		if name != nil {
			info.Name = *name
		}
		token := base64.StdEncoding.EncodeToString(secret[:])
		if err = store.insertToken(r.Context(), token, info); err != nil {
			return fail(err)
		}
		w.Header().Set("Content-Type", "application/json;charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"token": token, "info": tokenJSON(info, legacy)})
	}
	return true
}
