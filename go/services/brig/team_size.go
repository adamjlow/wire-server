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
	"strconv"
	"strings"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type teamSize struct {
	Regulars *big.Int `json:"teamSizeRegulars"`
	Apps     *big.Int `json:"teamSizeApps"`
	Total    *big.Int `json:"teamSize"`
}

// Aeson Natural accepts integer-valued Scientific JSON numbers. Avoid float64
// rounding and preserve arbitrary-size counts used in the admission guard.
func nativeNaturalCount(raw json.RawMessage) *big.Int {
	zero := func() *big.Int { return new(big.Int) }
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil {
		return zero()
	}
	number, ok := value.(json.Number)
	if !ok {
		return zero()
	}
	digits := string(number)
	exponent := 0
	if i := strings.IndexAny(digits, "eE"); i >= 0 {
		var err error
		exponent, err = strconv.Atoi(digits[i+1:])
		if err != nil {
			return zero()
		}
		digits = digits[:i]
	}
	fraction := 0
	if i := strings.IndexByte(digits, '.'); i >= 0 {
		fraction = len(digits) - i - 1
		digits = digits[:i] + digits[i+1:]
	}
	coefficient, ok := new(big.Int).SetString(digits, 10)
	if !ok || coefficient.Sign() < 0 {
		return zero()
	}
	if coefficient.Sign() == 0 {
		return coefficient
	}
	// A sufficiently negative exponent cannot be integral. Check before
	// subtracting the fractional length, avoiding machine-int underflow.
	if exponent < -len(digits) {
		return zero()
	}
	scale := exponent - fraction
	if scale < 0 {
		places := -scale
		if places > len(digits) || strings.Trim(digits[len(digits)-places:], "0") != "" {
			return zero()
		}
		coefficient, ok = new(big.Int).SetString(digits[:len(digits)-places], 10)
		if !ok {
			return zero()
		}
		return coefficient
	}
	return coefficient.Mul(coefficient, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(scale)), nil))
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
			Count json.RawMessage `json:"doc_count"`
		}
		if json.Unmarshal(result.Aggregations[name], &aggregate) != nil {
			return new(big.Int)
		}
		return nativeNaturalCount(aggregate.Count)
	}
	regulars, apps := count("regulars"), count("apps")
	return teamSize{Regulars: regulars, Apps: apps, Total: new(big.Int).Add(regulars, apps)}, nil
}
