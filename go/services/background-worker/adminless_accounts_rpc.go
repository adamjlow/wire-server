// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type adminlessBrigAccounts struct {
	Endpoint  Endpoint
	Client    *http.Client
	RequestID string
}

func retryAdminlessBrigTransport(err error) bool {
	if err == nil {
		return false
	}
	var operation *net.OpError
	if errors.As(err, &operation) {
		return true
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return true
	}
	var network net.Error
	if errors.As(err, &network) && network.Timeout() {
		return true
	}
	return errors.Is(err, io.EOF)
}
func (b adminlessBrigAccounts) Users(ctx context.Context, ids []string) ([]adminlessUser, error) {
	if b.Client == nil || !b.Endpoint.valid() {
		return nil, errors.New("Brig account lookup unavailable")
	}
	// Source empty chunk list issues no request. Each nonempty chunk retains input
	// order and duplicates; callers perform their own sorted ID de-duplication.
	canonical := make([]string, len(ids))
	for i, id := range ids {
		parsed, err := parseUserID(id)
		if err != nil {
			return nil, errors.New("invalid Brig account identifier")
		}
		canonical[i] = parsed.String()
	}
	var result []adminlessUser
	for start := 0; start < len(canonical); start += 100 {
		end := min(start+100, len(canonical))
		query := url.Values{"ids": {strings.Join(canonical[start:end], ",")}}
		target := "http://" + b.Endpoint.Address() + "/i/users?" + query.Encode()
		var chunk []adminlessUser
		for attempt := 0; attempt < 4; attempt++ {
			request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
			if err != nil {
				return nil, errors.New("Brig account request failed")
			}
			request.Header.Set("Request-Id", b.RequestID)
			response, err := b.Client.Do(request)
			retry := retryAdminlessBrigTransport(err)
			if err == nil {
				// Native expect2xx rejects status before decoding. Only server errors are
				// retryable; malformed successful JSON becomes an empty chunk without retry.
				status := response.StatusCode
				if status >= 200 && status < 300 {
					body, readErr := io.ReadAll(response.Body)
					_ = response.Body.Close()
					if readErr == nil {
						chunk = decodeAdminlessAccounts(body)
						break
					}
					err = readErr
					retry = retryAdminlessBrigTransport(err)
				} else {
					_ = response.Body.Close()
					err = errors.New("Brig account response failed")
					retry = status >= 500 && status < 600
				}
			} else if response != nil {
				_ = response.Body.Close()
			}
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if !retry || attempt == 3 {
				return nil, errors.New("Brig account lookup failed")
			}
			if !wait(ctx, (100*time.Millisecond)<<uint(attempt)) {
				return nil, ctx.Err()
			}
		}
		result = append(result, chunk...)
	}
	return result, nil
}
