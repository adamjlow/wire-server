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
	"strings"
	"syscall"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
)

func clientLockDeletion(c Config, sqs *userJournal) (func(context.Context, gocql.UUID, string) error, error) {
	if c.RandomPrekeys != nil && *c.RandomPrekeys {
		return func(context.Context, gocql.UUID, string) error { return nil }, nil
	}
	if c.AWS == nil || c.AWS.PrekeyTable == "" || sqs == nil {
		return nil, errors.New("prekey lock configuration unavailable")
	}
	endpoint := "https://dynamodb." + sqs.region + ".amazonaws.com/"
	if c.AWS.DynamoEndpoint != nil {
		endpoint = *c.AWS.DynamoEndpoint
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Hostname() == "" || parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("invalid prekey lock endpoint")
	}
	// Native AWSEndpoint keeps scheme/host/port, discarding path/query.
	endpoint = (&url.URL{Scheme: parsed.Scheme, Host: parsed.Host, Path: "/"}).String()
	return func(ctx context.Context, user gocql.UUID, id string) error {
		body, err := json.Marshal(map[string]any{"TableName": c.AWS.PrekeyTable, "Key": map[string]any{"client": map[string]string{"S": user.String() + "." + id}}})
		if err != nil {
			return errors.New("prekey lock encoding failed")
		}
		for attempt := 0; ; attempt++ {
			retry, err := deleteClientLockRequest(ctx, sqs, endpoint, body, attempt)
			if err == nil {
				return nil
			}
			if !retry || attempt >= 5 || ctx.Err() != nil {
				return err
			}
			if waitBrigRetry(ctx, nativeDynamoRetryDelay(attempt)) != nil {
				return errors.New("prekey lock request canceled")
			}
		}

	}, nil
}

// Pinned Amazonka DynamoDB defaults: base .05, growth2, five retries.
// retryService evaluates growth^(iteration-1), hence25ms on iteration0.
func nativeDynamoRetryDelay(attempt int) time.Duration { return 25 * time.Millisecond << attempt }

func deleteClientLockRequest(ctx context.Context, sqs *userJournal, endpoint string, body []byte, attempt int) (bool, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return false, errors.New("invalid prekey lock request")
	}
	request.Header.Set("Content-Type", "application/x-amz-json-1.0")
	request.Header.Set("X-Amz-Target", "DynamoDB_20120810.DeleteItem")
	credentials, err := sqs.credentials.Retrieve(ctx)
	if err != nil {
		return false, errors.New("prekey lock credentials unavailable")
	}
	sum := sha256.Sum256(body)
	if v4.NewSigner().SignHTTP(ctx, credentials, request, hex.EncodeToString(sum[:]), "dynamodb", sqs.region, sqs.now()) != nil {
		return false, errors.New("prekey lock signing failed")
	}
	response, err := sqs.client.Do(request)
	if err != nil {
		return attempt < 3 && nativeDynamoConnectionRetry(err), errors.New("prekey lock request failed")
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4<<20+1))
	if err != nil || len(raw) > 4<<20 {
		return false, errors.New("invalid prekey lock response")
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 || response.StatusCode == 304 {
		return false, nil
	}
	return nativeDynamoServiceRetry(response.StatusCode, response.Header, raw), errors.New("prekey lock deletion failed")
}

func nativeDynamoConnectionRetry(err error) bool {
	// Native env retryConnectionFailure excludes response timeout. Go's dial
	// failures and closed/no-response connection failures are equivalent classes.
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE) {
		return true
	}
	var op *net.OpError
	return errors.As(err, &op) && op.Op == "dial"
}

func nativeDynamoServiceRetry(status int, headers http.Header, raw []byte) bool {
	code := ""
	codeSet := false
	if len(raw) > 0 {
		// Native parseJSONError requires a JSON object. SerializeError is not
		// retryable, even when the HTTP status normally permits service retries.
		value, err := parsePropertyValue(raw)
		if err != nil {
			return false
		}
		canonical, err := json.Marshal(value)
		if err != nil {
			return false
		}
		fields, err := registrationObject(canonical)
		if err != nil {
			return false
		}
		text, err := registrationText(fields, "__type", false)
		if err != nil || text == nil {
			text, err = registrationText(fields, "code", false)
			if err != nil {
				return false
			}
		}
		if text != nil {
			code = nativeDynamoErrorCode(*text)
			codeSet = true
		}
		if code != "RequestEntityTooLarge" {
			message, err := registrationText(fields, "message", false)
			if err != nil || message == nil {
				if _, err = registrationText(fields, "Message", false); err != nil {
					return false
				}
			}
		}
	}
	if !codeSet {
		code = nativeDynamoErrorCode(strings.SplitN(headers.Get("X-Amzn-Errortype"), ":", 2)[0])
	}
	switch status {
	case 500, 502, 503, 504, 509, 429:
		return true
	case 400:
		switch code {
		case "RequestThrottled", "TransactionInProgress", "Throttled", "Throttling", "ProvisionedThroughputExceeded":
			return true
		}
	}
	return false
}

func nativeDynamoErrorCode(code string) string {
	if i := strings.IndexByte(code, '#'); i >= 0 {
		code = code[i+1:]
	}
	if strings.HasSuffix(code, "Exception") {
		return strings.TrimSuffix(code, "Exception")
	}
	return strings.TrimSuffix(code, "Fault")
}
