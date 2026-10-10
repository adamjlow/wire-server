// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
	"go.yaml.in/yaml/v3"
)

// TTL retains native Int32 arithmetic, including the round-to-even conversion
// of Scientific configuration values. Converting through float64 here would
// lose integer precision before the native wrap, changing persistent lifetimes.
type TTL int32

func (t *TTL) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.ScalarNode || (n.Tag != "!!int" && n.Tag != "!!float") {
		return errors.New("invalid TTL configuration")
	}
	r, ok := new(big.Rat).SetString(strings.ReplaceAll(n.Value, "_", ""))
	if !ok {
		return errors.New("invalid TTL configuration")
	}
	*t = TTL(roundInt32(r))
	return nil
}

func roundInt32(r *big.Rat) int32 {
	negative := r.Sign() < 0
	n := new(big.Int).Abs(r.Num())
	q, rem := new(big.Int), new(big.Int)
	q.QuoRem(n, r.Denom(), rem)
	cmp := new(big.Int).Lsh(rem, 1).Cmp(r.Denom())
	if cmp > 0 || (cmp == 0 && q.Bit(0) != 0) {
		q.Add(q, big.NewInt(1))
	}
	if negative {
		q.Neg(q)
	}
	q.Mod(q, new(big.Int).Lsh(big.NewInt(1), 32))
	return int32(uint32(q.Uint64()))
}

// Native NominalDiffTime is first converted to Double, then rounded to Int32.
func durationSeconds(seconds float64) int32 {
	r := new(big.Rat).SetFloat64(math.RoundToEven(seconds))
	return roundInt32(r)
}

func betweenSeconds(now, end time.Time) float64 {
	// time.Sub saturates for intervals exceeding a Duration. Native UTCTime does
	// not; subtract the integral seconds before rounding rather than Unix floats.
	return float64(end.Unix()-now.Unix()) + float64(end.Nanosecond()-now.Nanosecond())/1e9
}

type ttlError struct {
	table           string
	actual, maximum int32
	tooLong         bool
}

func (e *ttlError) Error() string {
	actual := fmt.Sprintf("TTL:%s:%d", e.table, e.actual)
	if e.tooLong {
		return fmt.Sprintf("TTLTooLong %q %q", actual, fmt.Sprintf("TTL:%s:%d", e.table, e.maximum))
	}
	return fmt.Sprintf("TTLNegative %q", actual)
}

func checkedTTL(table string, maximum TTL, now, end time.Time) (int32, error) {
	actual := durationSeconds(betweenSeconds(now, end))
	if actual > int32(maximum) {
		return 0, &ttlError{table: table, actual: actual, maximum: int32(maximum), tooLong: true}
	}
	if actual <= 0 {
		return 0, &ttlError{table: table, actual: actual, maximum: int32(maximum)}
	}
	return actual, nil
}

func (s cassandraStore) storeAuthRequest(ctx context.Context, id, issuer string, now, end time.Time, maximum TTL) error {
	ttl, err := checkedTTL("authreq", maximum, now, end)
	if err != nil {
		return err
	}
	return s.write(ctx, "INSERT INTO authreq (req, idp_issuer) VALUES (?,?) USING TTL ?", id, issuer, ttl)
}

func (s cassandraStore) authRequestIssuer(ctx context.Context, id string) (*string, error) {
	var issuer *string
	err := s.read(ctx, "SELECT idp_issuer FROM authreq WHERE req = ?", id).Scan(&issuer)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, nil
	}
	if err == nil && (issuer == nil || !absoluteURI(*issuer)) {
		return nil, errors.New("invalid stored SAML request")
	}
	return issuer, err
}

func (s cassandraStore) deleteAuthRequest(ctx context.Context, id string) error {
	return s.write(ctx, "DELETE FROM authreq WHERE req = ?", id)
}

func (s cassandraStore) storeAssertion(ctx context.Context, id string, now, end time.Time, maximum TTL) error {
	ttl, err := checkedTTL("authresp", maximum, now, end)
	if err != nil {
		return err
	}
	return s.write(ctx, "INSERT INTO authresp (resp) VALUES (?) USING TTL ?", id, ttl)
}

func (s cassandraStore) assertionAlive(ctx context.Context, id string) (bool, error) {
	var count *int64
	err := s.read(ctx, "SELECT COUNT(*) FROM authresp WHERE resp = ?", id).Scan(&count)
	if errors.Is(err, gocql.ErrNotFound) {
		return false, nil
	}
	if err == nil && count == nil {
		return false, errors.New("invalid stored SAML assertion")
	}
	return count != nil && *count == 1, err
}

func (s cassandraStore) deleteAssertion(ctx context.Context, id string) error {
	return s.write(ctx, "DELETE FROM authresp WHERE resp = ?", id)
}

type verdictFormat struct {
	Mobile                bool
	Success, Error, Label *string
}

func storedVerdict(con *int32, success, failure, label *string) (*verdictFormat, error) {
	if con == nil || (*con != 0 && *con != 1) {
		return nil, errors.New("invalid stored verdict constructor")
	}
	for _, uri := range []*string{success, failure} {
		if uri != nil && !absoluteURI(*uri) {
			return nil, errors.New("invalid stored verdict URI")
		}
	}
	if (*con == 0 && (success != nil || failure != nil)) || (*con == 1 && (success == nil || failure == nil)) {
		return nil, nil
	}
	return &verdictFormat{Mobile: *con == 1, Success: success, Error: failure, Label: label}, nil
}

func (s cassandraStore) storeVerdict(ctx context.Context, id string, seconds float64, format verdictFormat) error {
	var con int32
	if format.Mobile {
		con = 1
	}
	return s.write(ctx, "INSERT INTO verdict (req, format_con, format_mobile_success, format_mobile_error, cookie_label) VALUES (?, ?, ?, ?, ?) USING TTL ?", id, con, format.Success, format.Error, format.Label, durationSeconds(seconds)*2)
}

func (s cassandraStore) getVerdict(ctx context.Context, id string) (*verdictFormat, error) {
	var con *int32
	var success, failure, label *string
	err := s.read(ctx, "SELECT format_con, format_mobile_success, format_mobile_error, cookie_label FROM verdict WHERE req = ?", id).Scan(&con, &success, &failure, &label)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return storedVerdict(con, success, failure, label)
}
