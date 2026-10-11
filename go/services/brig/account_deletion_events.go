// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type accountDeletionEvents struct {
	domain          string
	page            func(context.Context, gocql.UUID, bool, *gocql.UUID, int, []byte) (connectionPage, error)
	notify          func(context.Context, []byte, string)
	connectionEvent func(context.Context, gocql.UUID, *string, string, storedConnection, *string) error
	enqueue         func(context.Context, string, []byte) error
	rescind         func(context.Context, string, gocql.UUID, gocql.UUID, string) error
	journal         *userJournal
	now             func() time.Time
}

func deletionNotificationBundle(domain string, user gocql.UUID, contacts []storedConnection, requestID string) ([]byte, error) {
	if !deletionEventDomain(domain) || len(contacts) < 1 || len(contacts) > 1000 {
		return nil, errors.New("invalid deletion notification bundle")
	}
	ids := make([]string, len(contacts))
	for i, contact := range contacts {
		ids[i] = contact.to.String()
	}
	body, err := json.Marshal(map[string]any{"user": user.String(), "connections": ids})
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{
		"notifications":            []any{map[string]any{"ownDomain": domain, "targetComponent": "brig", "path": "on-user-deleted-connections", "body": string(body), "bodyVersions": map[string]int{"from": 0}, "requestId": requestID}},
		"unsupportedVersionPolicy": "keep_queued",
	})
}

func (s accountDeletionEvents) direct(ctx context.Context, user gocql.UUID, recipients []gocql.UUID, requestID string) error {
	event := map[string]any{"type": "user.delete", "id": user.String(), "qualified_id": map[string]string{"id": user.String(), "domain": s.domain}}
	// Native chunkPushes splits the input list before Set.fromList sorts and
	// deduplicates each chunk's recipients. Preserve its bounded128 fanout.
	for start := 0; start < len(recipients); start += 128 {
		chunk := append([]gocql.UUID(nil), recipients[start:min(start+128, len(recipients))]...)
		slices.SortFunc(chunk, func(a, b gocql.UUID) int { return slices.Compare(a[:], b[:]) })
		chunk = slices.Compact(chunk)
		targets := make([]any, 0, len(chunk))
		for _, id := range chunk {
			targets = append(targets, map[string]any{"user_id": id.String(), "route": "direct", "clients": []string{}})
		}
		raw, err := json.Marshal([]any{map[string]any{"recipients": targets, "origin": user.String(), "payload": []any{event}, "is_cells_event": false}})
		if err != nil {
			return err
		}
		s.notify(ctx, raw, requestID)
	}
	return nil
}

func (s accountDeletionEvents) generate(ctx context.Context, user gocql.UUID, requestID string) error {
	if !deletionEventDomain(s.domain) || s.page == nil || s.notify == nil || s.connectionEvent == nil || s.enqueue == nil || s.rescind == nil || s.now == nil {
		return errors.New("account deletion events unavailable")
	}
	if err := s.direct(ctx, user, []gocql.UUID{user}, requestID); err != nil {
		return err
	}
	var start *gocql.UUID
	for {
		page, err := s.page(ctx, user, false, start, 501, nil)
		if err != nil {
			return err
		}
		rows := page.rows[:min(500, len(page.rows))]
		if len(rows) == 0 {
			break
		}
		var accepted []gocql.UUID
		for _, row := range rows {
			if row.relation == 0 {
				accepted = append(accepted, row.to)
			}
		}
		if err := s.direct(ctx, user, accepted, requestID); err != nil {
			return err
		}
		for _, row := range rows {
			if row.relation == 4 {
				row.from, row.to = row.to, user
				row.domain = s.domain
				row.relation = 5
				row.updated = s.now().UTC().Truncate(time.Millisecond)
				if err := s.connectionEvent(ctx, user, nil, requestID, row, nil); err != nil {
					return err
				}
			}
		}
		if len(page.state) == 0 {
			break
		}
		last := rows[0].to
		for _, row := range rows[1:] {
			if slices.Compare(row.to[:], last[:]) > 0 {
				last = row.to
			}
		}
		start = &last
	}
	var state []byte
	for {
		page, err := s.page(ctx, user, true, nil, 1000, state)
		if err != nil {
			return err
		}
		if len(page.rows) > 1000 {
			return errors.New("invalid deletion remote page")
		}
		buckets := map[string][]storedConnection{}
		for _, row := range page.rows {
			if !deletionEventDomain(row.domain) {
				return errors.New("invalid deletion contact domain")
			}
			buckets[row.domain] = append(buckets[row.domain], row)
		}
		domains := make([]string, 0, len(buckets))
		for domain := range buckets {
			domains = append(domains, domain)
		}
		slices.Sort(domains)
		for _, domain := range domains {
			rows := buckets[domain]
			bundle, err := deletionNotificationBundle(s.domain, user, rows, requestID)
			if err != nil {
				return err
			}
			if err := s.enqueue(ctx, domain, bundle); err != nil {
				return err
			}
			for _, row := range rows {
				if row.relation == 4 {
					// Native deliberately continues after a failed direct rescind;
					// the persistent deletion message remains the durable effect.
					_ = s.rescind(ctx, domain, user, row.to, requestID)
				}
			}
		}
		if len(page.state) == 0 {
			break
		}
		state = append([]byte(nil), page.state...)
	}
	if s.journal != nil {
		return s.journal.enqueue(ctx, encodeUserJournal(1, user, s.journal.now(), nil, nil, nil, nil))
	}
	return nil
}

func deletionEventDomain(value string) bool { _, err := parseEmailDomain(value); return err == nil }
