// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/aws/aws-sdk-go-v2/aws"
)

func TestClientDeletionNativeDynamoLockKeyAndLocalMode(t *testing.T) {
	calls := 0
	status := 200
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "POST" || r.URL.Path != "/" || r.URL.RawQuery != "" || r.Header.Get("X-Amz-Target") != "DynamoDB_20120810.DeleteItem" || r.Header.Get("Content-Type") != "application/x-amz-json-1.0" || !strings.Contains(r.Header.Get("Authorization"), "/eu-west-1/dynamodb/aws4_request") {
			t.Error("native signed Dynamo request changed")
		}
		var body struct {
			TableName string
			Key       map[string]map[string]string
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || body.TableName != "synthetic-prekeys" || len(body.Key) != 1 || body.Key["client"]["S"] != (gocql.UUID{}).String()+".a" {
			t.Error("native lock key changed")
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	endpoint := server.URL + "/ignored?ignored=yes"
	j := &userJournal{client: server.Client(), region: "eu-west-1", now: time.Now, credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
		return aws.Credentials{AccessKeyID: "synthetic", SecretAccessKey: "synthetic"}, nil
	})}
	c := Config{AWS: &brigAWSConfig{PrekeyTable: "synthetic-prekeys", DynamoEndpoint: &endpoint}}
	remove, err := clientLockDeletion(c, j)
	if err != nil {
		t.Fatal(err)
	}
	if err := remove(context.Background(), gocql.UUID{}, "a"); err != nil {
		t.Fatal(err)
	}
	status = 400
	if err := remove(context.Background(), gocql.UUID{}, "a"); err == nil {
		t.Fatal("Dynamo rejection hidden")
	}
	yes := true
	c.RandomPrekeys = &yes
	remove, err = clientLockDeletion(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := remove(context.Background(), gocql.UUID{}, "a"); err != nil || calls != 2 {
		t.Fatal("local mode must not issue Dynamo delete")
	}
	c.RandomPrekeys = nil
	c.AWS = nil
	if _, err := clientLockDeletion(c, j); err == nil {
		t.Fatal("missing optimistic lock configuration accepted")
	}
}

func TestPinnedAmazonkaDynamoServiceRetryPolicy(t *testing.T) {
	for _, tc := range []struct {
		name         string
		status       int
		body, header string
		retry        bool
	}{
		{"server", 500, `{}`, "", true},
		{"gateway", 502, "", "", true},
		{"timeout service", 504, `{}`, "", true},
		{"limit status", 509, `{}`, "", true},
		{"status throttle", 429, `{}`, "", true},
		{"throughput", 400, `{"__type":"com.amazonaws#ProvisionedThroughputExceededException"}`, "", true},
		{"header throttle", 400, "", "ThrottlingException:details", true},
		{"explicit empty code", 400, `{"__type":""}`, "ThrottlingException", false},
		{"code precedence", 400, `{"__type":"AccessDeniedException","code":"ThrottlingException"}`, "", false},
		{"wrong message", 500, `{"Message":12}`, "", false},
		{"message fallback", 500, `{"message":12,"Message":"synthetic"}`, "", true},
		{"malformed server response", 500, `not-json`, "", false},
		{"array server response", 503, `[]`, "", false},
		{"unselected server status", 501, `{}`, "", false},
		{"single suffix", 400, `{"__type":"ThrottlingFaultException"}`, "", false},
		{"transaction", 400, `{"code":"TransactionInProgressException"}`, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			headers := http.Header{}
			headers.Set("X-Amzn-Errortype", tc.header)
			if nativeDynamoServiceRetry(tc.status, headers, []byte(tc.body)) != tc.retry {
				t.Fatal("pinned native retry classification differs")
			}
		})
	}
	for n := 0; n < 5; n++ {
		if nativeDynamoRetryDelay(n) != (25 * time.Millisecond << n) {
			t.Fatal("native deterministic backoff differs")
		}
	}
	if !nativeDynamoConnectionRetry(io.EOF) || !nativeDynamoConnectionRetry(&net.OpError{Op: "dial", Err: errors.New("synthetic")}) || nativeDynamoConnectionRetry(context.DeadlineExceeded) {
		t.Fatal("connection/response-timeout classification differs")
	}
}

func TestClientDeletionDynamoRetriesActualSignedRequests(t *testing.T) {
	for _, tc := range []struct {
		name           string
		failures, want int
	}{
		{"eventual success", 2, 3}, {"bounded exhaustion", 10, 6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if !strings.Contains(r.Header.Get("Authorization"), "/dynamodb/aws4_request") {
					t.Error("retry not signed")
				}
				if calls <= tc.failures {
					w.WriteHeader(400)
					_, _ = w.Write([]byte(`{"__type":"ProvisionedThroughputExceededException"}`))
					return
				}
				_, _ = w.Write([]byte(`{}`))
			}))
			defer server.Close()
			endpoint := server.URL
			j := &userJournal{client: server.Client(), region: "eu-west-1", now: time.Now, credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
				return aws.Credentials{AccessKeyID: "synthetic", SecretAccessKey: "synthetic"}, nil
			})}
			remove, err := clientLockDeletion(Config{AWS: &brigAWSConfig{PrekeyTable: "synthetic-prekeys", DynamoEndpoint: &endpoint}}, j)
			if err != nil {
				t.Fatal(err)
			}
			err = remove(context.Background(), gocql.UUID{}, "a")
			if calls != tc.want || (err == nil) != (tc.failures < 5) {
				t.Fatalf("calls=%d error=%v", calls, err)
			}
		})
	}
}
