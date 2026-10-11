// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func TestInternalDeletionSQSNativeStandardMessageAndIntegrity(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "synthetic")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "synthetic")
	t.Setenv("AWS_REGION", "eu-west-1")
	var endpoint string
	sent := 0
	badMD5 := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ParseForm() != nil || !strings.Contains(r.Header.Get("Authorization"), "/eu-west-1/sqs/aws4_request") {
			t.Error("missing native signed query")
		}
		switch r.Form.Get("Action") {
		case "GetQueueUrl":
			if r.Form.Get("QueueName") != "synthetic-internal" {
				t.Error("wrong queue")
			}
			_, _ = w.Write([]byte(`<GetQueueUrlResponse><GetQueueUrlResult><QueueUrl>` + endpoint + `/synthetic-internal</QueueUrl></GetQueueUrlResult></GetQueueUrlResponse>`))
		case "SendMessage":
			sent++
			if r.Form.Get("QueueUrl") != endpoint+"/synthetic-internal" || r.Form.Get("MessageGroupId") != "" || r.Form.Get("MessageDeduplicationId") != "" {
				t.Error("standard queue received FIFO fields")
			}
			var body map[string]string
			if json.Unmarshal([]byte(r.Form.Get("MessageBody")), &body) != nil || body["type"] != "user.delete" || body["user"] != (gocql.UUID{}).String() {
				t.Error("native deletion event changed")
			}
			digest := md5.Sum([]byte(r.Form.Get("MessageBody")))
			md5Text := hex.EncodeToString(digest[:])
			if badMD5 {
				md5Text = "mismatch"
			}
			_, _ = w.Write([]byte(`<SendMessageResponse><SendMessageResult><MD5OfMessageBody>` + md5Text + `</MD5OfMessageBody></SendMessageResult></SendMessageResponse>`))
		default:
			t.Error("unexpected action")
		}
	}))
	defer server.Close()
	endpoint = server.URL
	queue, err := newInternalEventQueue(context.Background(), &internalEventsConfig{Type: "sqs", Name: "synthetic-internal"}, &brigAWSConfig{SQSEndpoint: endpoint}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if err := queue.enqueueUserDeletion(context.Background(), gocql.UUID{}); err != nil || sent != 1 {
		t.Fatal(err)
	}
	badMD5 = true
	if err := queue.enqueueUserDeletion(context.Background(), gocql.UUID{}); err == nil || sent != 2 {
		t.Fatal("native checksum failure was hidden/retried")
	}
}

func TestClientDeletionSQSNullableConnectionAndChecksum(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "synthetic")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "synthetic")
	t.Setenv("AWS_REGION", "eu-west-1")
	var endpoint string
	connection := "synthetic-connection"
	var wantConnection *string
	bad := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if !strings.Contains(r.Header.Get("Authorization"), "/eu-west-1/sqs/aws4_request") {
			t.Error("unsigned SQS request")
		}
		if r.Form.Get("Action") == "GetQueueUrl" {
			_, _ = w.Write([]byte(`<GetQueueUrlResponse><GetQueueUrlResult><QueueUrl>` + endpoint + `/synthetic-internal</QueueUrl></GetQueueUrlResult></GetQueueUrlResponse>`))
			return
		}
		var body map[string]json.RawMessage
		raw := []byte(r.Form.Get("MessageBody"))
		if json.Unmarshal(raw, &body) != nil || len(body) != 4 || string(body["type"]) != `"client.delete"` || string(body["client"]) != `"a"` || string(body["user"]) != `"`+(gocql.UUID{}).String()+`"` {
			t.Error("native client deletion message changed")
		}
		want := `null`
		if wantConnection != nil {
			want = `"` + *wantConnection + `"`
		}
		if string(body["connection"]) != want || r.Form.Get("MessageGroupId") != "" || r.Form.Get("MessageDeduplicationId") != "" {
			t.Error("nullable connection/standard queue changed")
		}
		digest := md5.Sum(raw)
		checksum := hex.EncodeToString(digest[:])
		if bad {
			checksum = "mismatch"
		}
		_, _ = w.Write([]byte(`<SendMessageResponse><SendMessageResult><MD5OfMessageBody>` + checksum + `</MD5OfMessageBody></SendMessageResult></SendMessageResponse>`))
	}))
	defer server.Close()
	endpoint = server.URL
	q, err := newInternalEventQueue(context.Background(), &internalEventsConfig{Type: "sqs", Name: "synthetic-internal"}, &brigAWSConfig{SQSEndpoint: endpoint}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if err := q.enqueueClientDeletion(context.Background(), gocql.UUID{}, "a", nil); err != nil {
		t.Fatal(err)
	}
	wantConnection = &connection
	if err := q.enqueueClientDeletion(context.Background(), gocql.UUID{}, "a", &connection); err != nil {
		t.Fatal(err)
	}
	bad = true
	if err := q.enqueueClientDeletion(context.Background(), gocql.UUID{}, "a", &connection); err == nil {
		t.Fatal("native response checksum failure hidden")
	}
}
