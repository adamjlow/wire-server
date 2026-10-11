// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
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
