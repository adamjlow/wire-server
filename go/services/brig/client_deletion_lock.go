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
	"net/http"
	"net/url"

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
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return errors.New("invalid prekey lock request")
		}
		request.Header.Set("Content-Type", "application/x-amz-json-1.0")
		request.Header.Set("X-Amz-Target", "DynamoDB_20120810.DeleteItem")
		credentials, err := sqs.credentials.Retrieve(ctx)
		if err != nil {
			return errors.New("prekey lock credentials unavailable")
		}
		sum := sha256.Sum256(body)
		if v4.NewSigner().SignHTTP(ctx, credentials, request, hex.EncodeToString(sum[:]), "dynamodb", sqs.region, sqs.now()) != nil {
			return errors.New("prekey lock signing failed")
		}
		response, err := sqs.client.Do(request)
		if err != nil {
			return errors.New("prekey lock request failed")
		}
		defer response.Body.Close()
		_, err = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<20))
		if err != nil || response.StatusCode < 200 || response.StatusCode >= 300 {
			return errors.New("prekey lock deletion failed")
		}
		return nil
	}, nil
}
