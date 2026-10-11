// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/aws/aws-sdk-go-v2/aws"
)

func TestNativeRandomPrekeyExcludesLastResort(t *testing.T) {
	for _, keys := range [][]uncheckedPrekey{nil, {{ID: 65535, Key: "last"}}, {{ID: 7, Key: "ordinary"}, {ID: 65535, Key: "last"}}, {{ID: 65535, Key: "last"}, {ID: 7, Key: "ordinary"}}} {
		key, err := pickNativeRandomPrekey(keys)
		if err != nil {
			t.Fatal(err)
		}
		if len(keys) == 0 {
			if key != nil {
				t.Fatal("empty selection")
			}
			continue
		}
		if len(keys) == 1 {
			if key.ID != 65535 {
				t.Fatal("last resort missing")
			}
			continue
		}
		if key.ID != 7 {
			t.Fatal("last resort selected before ordinary key")
		}
	}
}

func TestNativeOptimisticClaimOrderingAndExhaustion(t *testing.T) {
	for _, successAt := range []int{1, 3, 0} {
		sequence := []string{}
		calls, failed, exhausted := 0, 0, 0
		version := uint32(4)
		key, err := optimisticPrekeyClaim(context.Background(), func() *uint32 { sequence = append(sequence, "get"); return &version }, func() (*uncheckedPrekey, error) {
			calls++
			sequence = append(sequence, "claim")
			return &uncheckedPrekey{ID: uint16(calls), Key: "synthetic"}, nil
		}, func(v *uint32) bool {
			sequence = append(sequence, "put")
			if v == nil || *v != 4 {
				t.Fatal("version lost")
			}
			return calls == successAt
		}, func() { failed++ }, func() { exhausted++ })
		want := successAt
		if want == 0 {
			want = 11
		}
		if err != nil || calls != want || key.ID != uint16(want) || failed != want-1 || exhausted != map[bool]int{true: 1, false: 0}[successAt == 0] {
			t.Fatal("native retry/final result changed")
		}
		if strings.Join(sequence, ",") != strings.TrimSuffix(strings.Repeat("get,claim,put,", want), ",") {
			t.Fatal("native get/action/conditional-put order changed")
		}
	}
	calls := 0
	_, err := optimisticPrekeyClaim(context.Background(), func() *uint32 { return nil }, func() (*uncheckedPrekey, error) { return nil, errors.New("synthetic Cassandra failure") }, func(*uint32) bool { calls++; return true }, nil, nil)
	if err == nil || calls != 0 {
		t.Fatal("storage failure did not stop before Dynamo write")
	}
}

func TestNativeDynamoPrekeyVersionAndConditionalWrite(t *testing.T) {
	user := gocql.TimeUUID()
	calls := []string{}
	versionText := "4294967295"
	reject := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target := r.Header.Get("X-Amz-Target")
		calls = append(calls, target)
		if r.URL.Path != "/" || r.URL.RawQuery != "" || r.Header.Get("Content-Type") != "application/x-amz-json-1.0" || !strings.Contains(r.Header.Get("Authorization"), "/eu-west-1/dynamodb/aws4_request") {
			t.Error("native signed Dynamo request changed")
		}
		var body map[string]json.RawMessage
		if json.NewDecoder(r.Body).Decode(&body) != nil || string(body["TableName"]) != `"synthetic-prekeys"` {
			t.Error("Dynamo request schema")
		}
		if strings.HasSuffix(target, "GetItem") {
			if string(body["ConsistentRead"]) != "true" || !strings.Contains(string(body["Key"]), user.String()+".a") {
				t.Error("consistent read/key changed")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"Item": map[string]any{"version": map[string]string{"N": versionText}}})
			return
		}
		if reject {
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"__type":"ConditionalCheckFailedException"}`))
			return
		}
		var expected map[string]struct {
			ComparisonOperator string
			AttributeValueList []map[string]string
		}
		_ = json.Unmarshal(body["Expected"], &expected)
		var item map[string]map[string]string
		_ = json.Unmarshal(body["Item"], &item)
		if item["client"]["S"] != user.String()+".a" {
			t.Error("put key changed")
		}
		if expected["version"].ComparisonOperator == "EQ" {
			if item["version"]["N"] != "0" || expected["version"].AttributeValueList[0]["N"] != "4294967295" {
				t.Error("Word32 increment/wire condition changed")
			}
		} else if expected["version"].ComparisonOperator != "NULL" || item["version"]["N"] != "1" {
			t.Error("missing version condition changed")
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	endpoint := server.URL + "/ignored?ignored=yes"
	journal := &userJournal{client: server.Client(), region: "eu-west-1", now: time.Now, credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
		return aws.Credentials{AccessKeyID: "synthetic", SecretAccessKey: "synthetic"}, nil
	})}
	store, err := newPrekeyClaimStore(Config{AWS: &brigAWSConfig{PrekeyTable: "synthetic-prekeys", DynamoEndpoint: &endpoint}}, clientStore{}, journal)
	if err != nil {
		t.Fatal(err)
	}
	version := store.version(context.Background(), user, "a")
	if version == nil || *version != 4294967295 || !store.advance(context.Background(), user, "a", version) {
		t.Fatal("Dynamo version/increment failed")
	}
	versionText = "invalid"
	if store.version(context.Background(), user, "a") != nil || !store.advance(context.Background(), user, "a", nil) {
		t.Fatal("native unparseable version fallback changed")
	}
	reject = true
	if store.advance(context.Background(), user, "a", nil) || len(calls) != 5 {
		t.Fatal("conditional failure became success/retry")
	}
	yes := true
	local, err := newPrekeyClaimStore(Config{RandomPrekeys: &yes}, clientStore{}, nil)
	if err != nil || !local.random || len(local.gate) != 1 || local.version != nil {
		t.Fatal("random mode must not require Dynamo")
	}
}
