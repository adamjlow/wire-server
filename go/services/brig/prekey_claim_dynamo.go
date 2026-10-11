// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
)

type prekeyDynamoLock struct {
	journal         *userJournal
	endpoint, table string
}

func newPrekeyClaimStore(c Config, store clientStore, journal *userJournal) (prekeyClaimStore, error) {
	s := prekeyClaimStore{store: store, random: c.RandomPrekeys != nil && *c.RandomPrekeys}
	if s.random {
		s.gate = make(chan struct{}, 1)
		s.gate <- struct{}{}
		return s, nil
	}
	if c.AWS == nil || c.AWS.PrekeyTable == "" || journal == nil {
		return s, errors.New("prekey lock configuration unavailable")
	}
	endpoint := "https://dynamodb." + journal.region + ".amazonaws.com/"
	if c.AWS.DynamoEndpoint != nil {
		endpoint = *c.AWS.DynamoEndpoint
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Hostname() == "" || parsed.Scheme != "http" && parsed.Scheme != "https" {
		return s, errors.New("invalid prekey lock endpoint")
	}
	lock := prekeyDynamoLock{journal: journal, table: c.AWS.PrekeyTable, endpoint: (&url.URL{Scheme: parsed.Scheme, Host: parsed.Host, Path: "/"}).String()}
	s.version, s.advance = lock.version, lock.advance
	return s, nil
}

func prekeyDynamoKey(user gocql.UUID, id string) map[string]any {
	return map[string]any{"client": map[string]string{"S": user.String() + "." + id}}
}

func (s prekeyDynamoLock) version(ctx context.Context, user gocql.UUID, id string) *uint32 {
	raw, err := s.request(ctx, "GetItem", map[string]any{"TableName": s.table, "Key": prekeyDynamoKey(user, id), "ConsistentRead": true})
	if err != nil {
		return nil
	}
	var response struct {
		Item map[string]map[string]json.RawMessage
	}
	if json.Unmarshal(raw, &response) != nil {
		return nil
	}
	var text string
	if json.Unmarshal(response.Item["version"]["N"], &text) != nil {
		return nil
	}
	n, err := strconv.ParseUint(strings.TrimSpace(text), 10, 32)
	if err != nil {
		return nil
	}
	version := uint32(n)
	return &version
}

func (s prekeyDynamoLock) advance(ctx context.Context, user gocql.UUID, id string, version *uint32) bool {
	item := prekeyDynamoKey(user, id)
	next := uint32(1)
	condition := map[string]any{"ComparisonOperator": "NULL"}
	if version != nil {
		next = *version + 1
		condition = map[string]any{"ComparisonOperator": "EQ", "AttributeValueList": []any{map[string]string{"N": strconv.FormatUint(uint64(*version), 10)}}}
	}
	item["version"] = map[string]string{"N": strconv.FormatUint(uint64(next), 10)}
	_, err := s.request(ctx, "PutItem", map[string]any{"TableName": s.table, "Item": item, "Expected": map[string]any{"version": condition}})
	return err == nil
}

type prekeyDynamoHTTPError struct{ retry bool }

func (prekeyDynamoHTTPError) Error() string { return "prekey lock transport failed" }

func (s prekeyDynamoLock) request(ctx context.Context, operation string, input map[string]any) ([]byte, error) {
	// Native execDyn recovering: HTTP exceptions have an outer three-retry
	// 100/200/400ms policy, separate from Amazonka's service retry defaults.
	for outer := 0; ; outer++ {
		raw, err := s.requestSDK(ctx, operation, input)
		if err == nil {
			return raw, nil
		}
		var transport prekeyDynamoHTTPError
		if !errors.As(err, &transport) || !transport.retry || outer >= 3 || ctx.Err() != nil {
			return nil, err
		}
		if waitBrigRetry(ctx, 100*time.Millisecond<<outer) != nil {
			return nil, errors.New("prekey lock request canceled")
		}
	}
}

func (s prekeyDynamoLock) requestSDK(ctx context.Context, operation string, input map[string]any) ([]byte, error) {
	body, err := json.Marshal(input)
	if err != nil {
		return nil, errors.New("prekey lock encoding failed")
	}
	for attempt := 0; ; attempt++ {
		raw, retry, err := s.requestOnce(ctx, operation, body, attempt)
		if err == nil {
			return raw, nil
		}
		if !retry || attempt >= 5 || ctx.Err() != nil {
			return nil, err
		}
		if waitBrigRetry(ctx, nativeDynamoRetryDelay(attempt)) != nil {
			return nil, errors.New("prekey lock request canceled")
		}
	}
}

func (s prekeyDynamoLock) requestOnce(ctx context.Context, operation string, body []byte, attempt int) ([]byte, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, false, errors.New("invalid prekey lock request")
	}
	req.Header.Set("Content-Type", "application/x-amz-json-1.0")
	req.Header.Set("X-Amz-Target", "DynamoDB_20120810."+operation)
	credentials, err := s.journal.credentials.Retrieve(ctx)
	if err != nil {
		return nil, false, errors.New("prekey lock credentials unavailable")
	}
	sum := sha256.Sum256(body)
	if v4.NewSigner().SignHTTP(ctx, credentials, req, hex.EncodeToString(sum[:]), "dynamodb", s.journal.region, s.journal.now()) != nil {
		return nil, false, errors.New("prekey lock signing failed")
	}
	response, err := s.journal.client.Do(req)
	if err != nil {
		var network net.Error
		outerRetry := nativeDynamoConnectionRetry(err) || errors.As(err, &network) && network.Timeout()
		return nil, attempt < 3 && nativeDynamoConnectionRetry(err), prekeyDynamoHTTPError{retry: outerRetry}
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4<<20+1))
	if err != nil || len(raw) > 4<<20 {
		return nil, false, errors.New("invalid prekey lock response")
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 || response.StatusCode == 304 {
		// Pinned Amazonka receiveJSON decodes an object even for PutItem.
		// A successful HTTP status with an invalid body is SerializeError.
		if _, err := registrationObject(raw); err != nil {
			return nil, false, errors.New("invalid prekey lock response")
		}
		return raw, false, nil
	}
	return nil, nativeDynamoServiceRetry(response.StatusCode, response.Header, raw), errors.New("prekey lock request rejected")
}
