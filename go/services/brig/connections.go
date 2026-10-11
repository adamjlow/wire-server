// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type storedConnection struct {
	from, to           gocql.UUID
	domain             string
	relation           int32
	updated            time.Time
	conversation       *gocql.UUID
	conversationDomain string
}

func connectionRelation(relation int32) (string, error) {
	if relation >= 6 && relation <= 11 {
		return "missing-legalhold-consent", nil
	}
	if relation < 0 || relation > 5 {
		return "", errors.New("invalid stored connection relation")
	}
	return []string{"accepted", "blocked", "pending", "ignored", "sent", "cancelled"}[relation], nil
}
func (c storedConnection) wire() (map[string]any, error) {
	relation, err := connectionRelation(c.relation)
	if err != nil {
		return nil, err
	}
	result := map[string]any{"from": c.from.String(), "to": c.to.String(), "qualified_to": map[string]string{"id": c.to.String(), "domain": c.domain}, "status": relation, "last_update": c.updated.UTC().Format("2006-01-02T15:04:05.000Z")}
	if c.conversation != nil {
		result["conversation"] = c.conversation.String()
		result["qualified_conversation"] = map[string]string{"id": c.conversation.String(), "domain": c.conversationDomain}
	}
	return result, nil
}

type connectionPage struct {
	rows  []storedConnection
	state []byte
}
type connectionStore struct {
	cassandra *gocql.Session
	domain    string
}

func (s connectionStore) get(ctx context.Context, user, target gocql.UUID, domain string) (*storedConnection, error) {
	result := storedConnection{from: user, to: target, domain: domain, conversationDomain: s.domain}
	var status *int32
	var updated *time.Time
	query := `SELECT left, right, status, last_update, conv FROM connection WHERE left = ? AND right = ?`
	args := []any{user, target}
	var err error
	if domain == s.domain {
		var from, to gocql.UUID
		err = s.cassandra.Query(query, args...).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).Scan(&from, &to, &status, &updated, &result.conversation)
		result.from = from
		result.to = to
	} else {
		var conversationDomain *string
		err = s.cassandra.Query(`SELECT status, last_update, conv_domain, conv_id FROM connection_remote where left = ? AND right_domain = ? AND right_user = ?`, user, domain, target).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).Scan(&status, &updated, &conversationDomain, &result.conversation)
		if err == nil && (conversationDomain == nil || result.conversation == nil) {
			return nil, errors.New("invalid remote connection")
		}
		if conversationDomain != nil {
			result.conversationDomain = *conversationDomain
		}
	}
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if status == nil || updated == nil {
		return nil, errors.New("invalid stored connection")
	}
	result.relation = *status
	result.updated = *updated
	if _, err := result.wire(); err != nil {
		return nil, err
	}
	return &result, nil
}
func (s connectionStore) page(ctx context.Context, user gocql.UUID, remote bool, start *gocql.UUID, size int, state []byte) (connectionPage, error) {
	result := connectionPage{rows: []storedConnection{}}
	query := `SELECT left, right, status, last_update, conv FROM connection WHERE left = ? ORDER BY right ASC`
	args := []any{user}
	if start != nil {
		query = `SELECT left, right, status, last_update, conv FROM connection WHERE left = ? AND right > ? ORDER BY right ASC`
		args = append(args, *start)
	}
	if remote {
		query = `SELECT right_domain, right_user, status, last_update, conv_domain, conv_id FROM connection_remote where left = ?`
	}
	// Explicit PageState disables driver's automatic pagination. Preserve the
	// server's opaque cursor; no application offset or synthetic page token.
	iter := s.cassandra.Query(query, args...).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).PageSize(size).PageState(state).Iter()
	var from, to *gocql.UUID
	var domain, conversationDomain *string
	var status *int32
	var updated *time.Time
	var conversation *gocql.UUID
	for {
		var found bool
		if remote {
			found = iter.Scan(&domain, &to, &status, &updated, &conversationDomain, &conversation)
		} else {
			found = iter.Scan(&from, &to, &status, &updated, &conversation)
		}
		if !found {
			break
		}
		if to == nil || status == nil || updated == nil || !remote && from == nil || remote && (domain == nil || conversationDomain == nil || conversation == nil) {
			_ = iter.Close()
			return result, errors.New("invalid stored connection")
		}
		row := storedConnection{from: user, to: *to, domain: s.domain, relation: *status, updated: *updated, conversation: conversation, conversationDomain: s.domain}
		if remote {
			row.domain = *domain
			row.conversationDomain = *conversationDomain
		} else {
			row.from = *from
		}
		if _, err := row.wire(); err != nil {
			_ = iter.Close()
			return result, err
		}
		if conversation != nil {
			id := *conversation
			row.conversation = &id
		}
		result.rows = append(result.rows, row)
	}
	result.state = append([]byte{}, iter.PageState()...)
	if err := iter.Close(); err != nil {
		return result, err
	}
	return result, nil
}
func (s connectionStore) insert(ctx context.Context, c storedConnection) error {
	if c.conversation == nil {
		return errors.New("connection insertion requires conversation")
	}
	query := `INSERT INTO connection (left, right, status, last_update, conv) VALUES (?, ?, ?, ?, ?)`
	args := []any{c.from, c.to, c.relation, c.updated.UTC().Truncate(time.Millisecond), *c.conversation}
	if c.domain != s.domain {
		query = `INSERT INTO connection_remote (left, right_domain, right_user, status, last_update, conv_domain, conv_id) VALUES (?, ?, ?, ?, ?, ?, ?)`
		args = []any{c.from, c.domain, c.to, c.relation, c.updated.UTC().Truncate(time.Millisecond), c.conversationDomain, *c.conversation}
	}
	return s.cassandra.Query(query, args...).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5}).Exec()
}
func (s connectionStore) update(ctx context.Context, c storedConnection) error {
	query := `UPDATE connection SET status = ?, last_update = ? WHERE left = ? AND right = ?`
	args := []any{c.relation, c.updated.UTC().Truncate(time.Millisecond), c.from, c.to}
	if c.domain != s.domain {
		query = `UPDATE connection_remote set status = ?, last_update = ? WHERE left = ? and right_domain = ? and right_user = ?`
		args = []any{c.relation, c.updated.UTC().Truncate(time.Millisecond), c.from, c.domain, c.to}
	}
	return s.cassandra.Query(query, args...).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5}).Exec()
}

type connectionReads struct {
	domain string
	get    func(context.Context, gocql.UUID, gocql.UUID, string) (*storedConnection, error)
	page   func(context.Context, gocql.UUID, bool, *gocql.UUID, int, []byte) (connectionPage, error)
}

func connectionWireRows(rows []storedConnection) ([]map[string]any, error) {
	result := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		wire, err := row.wire()
		if err != nil {
			return nil, err
		}
		result = append(result, wire)
	}
	return result, nil
}
func (s connectionReads) paged(ctx context.Context, user gocql.UUID, size int, table byte, state []byte) (map[string]any, error) {
	rows := []storedConnection{}
	if table == 0 {
		local, err := s.page(ctx, user, false, nil, size, state)
		if err != nil {
			return nil, err
		}
		rows = local.rows
		if len(local.state) > 0 || len(rows) >= size {
			wire, err := connectionWireRows(rows)
			return map[string]any{"connections": wire, "has_more": true, "paging_state": base64.URLEncoding.EncodeToString(append([]byte{0}, local.state...))}, err
		}
		size -= len(rows)
		state = nil
	}
	remote, err := s.page(ctx, user, true, nil, size, state)
	if err != nil {
		return nil, err
	}
	rows = append(rows, remote.rows...)
	wire, err := connectionWireRows(rows)
	return map[string]any{"connections": wire, "has_more": len(remote.state) > 0, "paging_state": base64.URLEncoding.EncodeToString(append([]byte{1}, remote.state...))}, err
}
func (s connectionReads) handle(w http.ResponseWriter, r *http.Request, path string) bool {
	version, _ := strconv.Atoi(r.Header.Get("X-Wire-API-Version"))
	parts := strings.Split(strings.Trim(path, "/"), "/")
	legacyList := path == "/connections" && version < 2
	get := len(parts) == 2 && parts[0] == "connections" && version < 2 || len(parts) == 3 && parts[0] == "connections"
	paged := path == "/list-connections"
	if !legacyList && !get && !paged {
		return false
	}
	// Mutations are dispatched by the connection write service once complete.
	if !paged && r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	if paged && r.Method != http.MethodPost {
		w.WriteHeader(405)
		return true
	}
	user, ok := userHeader(w, r)
	if !ok {
		return true
	}
	if get {
		domain := s.domain
		targetText := parts[1]
		if len(parts) == 3 {
			var err error
			domain, err = parseEmailDomain(parts[1])
			if err != nil {
				routeParseError(w, "invalid domain")
				return true
			}
			targetText = parts[2]
		}
		canonical, ok := tokenUUID(targetText)
		if !ok {
			routeParseError(w, "invalid UUID")
			return true
		}
		target, _ := gocql.ParseUUID(canonical)
		row, err := s.get(r.Context(), user, target, domain)
		if err != nil {
			authenticationHTTPError(w, err)
		} else if row == nil {
			w.WriteHeader(404)
		} else {
			wire, err := row.wire()
			if err != nil {
				authenticationHTTPError(w, err)
			} else {
				readJSON(w, r, wire)
			}
		}
		return true
	}
	if legacyList {
		size := 100
		var start *gocql.UUID
		if values, present := r.URL.Query()["size"]; present && len(values) > 0 {
			n, err := strconv.ParseInt(values[0], 10, 32)
			if err != nil || n < 1 || n > 500 {
				routeParseError(w, "Invalid size")
				return true
			}
			size = int(n)
		}
		if values, present := r.URL.Query()["start"]; present && len(values) > 0 {
			canonical, ok := tokenUUID(values[0])
			if !ok {
				routeParseError(w, "invalid UUID")
				return true
			}
			id, _ := gocql.ParseUUID(canonical)
			start = &id
		}
		page, err := s.page(r.Context(), user, false, start, size+1, nil)
		if err != nil {
			authenticationHTTPError(w, err)
			return true
		}
		rows := page.rows
		if len(rows) > size {
			rows = rows[:size]
		}
		wire, err := connectionWireRows(rows)
		if err != nil {
			authenticationHTTPError(w, err)
		} else {
			readJSON(w, r, map[string]any{"connections": wire, "has_more": len(page.state) > 0})
		}
		return true
	}
	if !publicClientContentType(w, r) {
		return true
	}
	raw, err := publicClientJSON(r)
	if err != nil {
		routeParseError(w, "Invalid connection page request")
		return true
	}
	fields, err := registrationObject(raw)
	if err != nil {
		routeParseError(w, "Invalid connection page request")
		return true
	}
	size := 100
	table := byte(0)
	var state []byte
	if registrationPresent(fields["size"]) {
		n, err := federationInteger(fields["size"])
		if err != nil || n < 1 || n > 500 {
			routeParseError(w, "Invalid connection page size")
			return true
		}
		size = int(n)
	}
	if registrationPresent(fields["paging_state"]) {
		text, err := registrationText(fields, "paging_state", true)
		if err != nil {
			routeParseError(w, "Invalid connection paging state")
			return true
		}
		decoded, err := base64.URLEncoding.Strict().DecodeString(*text)
		if err != nil || len(decoded) == 0 || decoded[0] > 1 {
			routeParseError(w, "Invalid connection paging state")
			return true
		}
		table = decoded[0]
		state = decoded[1:]
	}
	result, err := s.paged(r.Context(), user, size, table, state)
	if err != nil {
		authenticationHTTPError(w, err)
	} else {
		readJSON(w, r, result)
	}
	return true
}
