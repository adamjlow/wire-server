// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type teamSize struct {
	Regulars *big.Int `json:"teamSizeRegulars"`
	Apps     *big.Int `json:"teamSizeApps"`
	Total    *big.Int `json:"teamSize"`
}

// Read only the primary index, as native getTeamSizeImpl does. Legacy
// documents without type count as regular users; do not add status filters.
func (s *searchIndexWriter) teamSize(ctx context.Context, team gocql.UUID, requestID string) (teamSize, error) {
	failure := errors.New("search index lookup failed")
	if s == nil || len(s.connections) == 0 {
		return teamSize{}, failure
	}
	term := func(field, value string) any { return map[string]any{"term": map[string]string{field: value}} }
	teamQuery := term("team", team.String())
	body, err := json.Marshal(map[string]any{
		"size": 0,
		"aggs": map[string]any{
			"regulars": map[string]any{"filter": map[string]any{"bool": map[string]any{"must": []any{
				teamQuery, map[string]any{"bool": map[string]any{"should": []any{
					term("type", "regular"), map[string]any{"bool": map[string]any{"must_not": []any{map[string]any{"exists": map[string]string{"field": "type"}}}}},
				}}},
			}}}},
			"apps": map[string]any{"filter": map[string]any{"bool": map[string]any{"must": []any{teamQuery, term("type", "app")}}}},
		},
	})
	if err != nil {
		return teamSize{}, failure
	}
	conn := s.connections[0]
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, conn.base.JoinPath(conn.index, "user", "_search").String(), bytes.NewReader(body))
	if err != nil {
		return teamSize{}, failure
	}
	req.Header.Set("Content-Type", "application/json")
	if requestID != "" {
		req.Header.Set("Request-Id", requestID)
	}
	if conn.username != nil {
		req.SetBasicAuth(*conn.username, *conn.password)
	}
	response, err := conn.client.Do(req)
	if err != nil {
		return teamSize{}, failure
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return teamSize{}, failure
	}
	var result struct {
		Aggregations map[string]json.RawMessage `json:"aggregations"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 4<<20))
	if decoder.Decode(&result) != nil {
		return teamSize{}, failure
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return teamSize{}, failure
	}
	count := func(name string) *big.Int {
		// Native parseMaybe defaults absent/malformed FilterResult to zero.
		var aggregate struct {
			Count *big.Int `json:"doc_count"`
		}
		if json.Unmarshal(result.Aggregations[name], &aggregate) != nil || aggregate.Count == nil || aggregate.Count.Sign() < 0 {
			return new(big.Int)
		}
		return aggregate.Count
	}
	regulars, apps := count("regulars"), count("apps")
	return teamSize{Regulars: regulars, Apps: apps, Total: new(big.Int).Add(regulars, apps)}, nil
}
