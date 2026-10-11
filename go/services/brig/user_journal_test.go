// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func TestUserJournalNativeProto(t *testing.T) {
	email, locale, name := "e", "en", "n"
	team := gocql.UUID{}
	raw := encodeUserJournal(0, team, time.Unix(0, 0), &email, &locale, &name, &team)
	const expected = "080012100000000000000000000000000000000018002201652a02656e3210000000000000000000000000000000003a016e"
	if hex.EncodeToString(raw) != expected {
		t.Fatal("native proto2 required/optional fields changed")
	}
	for _, sample := range []struct {
		seconds int64
		nanos   int64
		want    byte
	}{{0, 500000000, 0}, {1, 500000000, 2}, {2, 500000000, 2}, {0, 500000001, 1}} {
		raw := encodeUserJournal(2, team, time.Unix(sample.seconds, sample.nanos), nil, nil, nil, nil)
		if len(raw) != 22 || raw[20] != 24 || raw[21] != sample.want {
			t.Fatal("journal timestamp no longer rounds to nearest even second")
		}
	}
}
func TestUserJournalSQSDiscoveryAndStableFIFOFields(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "synthetic-access")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "synthetic-secret")
	t.Setenv("AWS_REGION", "us-east-1")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	sent := 0
	var previous url.Values
	var endpoint string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
			t.Error("journal request not signed")
		}
		if err := r.ParseForm(); err != nil {
			t.Error("invalid journal query")
		}
		w.Header().Set("Content-Type", "text/xml")
		switch r.Form.Get("Action") {
		case "GetQueueUrl":
			if r.URL.Path != "/" || r.Form.Get("QueueName") != "synthetic-events.fifo" {
				t.Error("native queue discovery changed")
			}
			w.Write([]byte(`<GetQueueUrlResponse><GetQueueUrlResult><QueueUrl>` + endpoint + `/queue/synthetic-events.fifo</QueueUrl></GetQueueUrlResult></GetQueueUrlResponse>`))
		case "SendMessage":
			sent++
			if r.Form.Get("QueueUrl") != endpoint+"/queue/synthetic-events.fifo" || r.Form.Get("MessageGroupId") != "user.events" || r.Form.Get("MessageBody") != "c3ludGhldGlj" {
				t.Error("native FIFO payload changed")
			}
			if _, err := gocql.ParseUUID(r.Form.Get("MessageDeduplicationId")); err != nil {
				t.Error("missing UUID deduplication")
			}
			if previous != nil && previous.Encode() != r.Form.Encode() {
				t.Error("retry changed FIFO deduplication or body")
			}
			previous = r.Form
			if sent == 1 {
				w.WriteHeader(400)
				w.Write([]byte(`<ErrorResponse><Error><Code>RequestThrottled</Code></Error></ErrorResponse>`))
			} else {
				w.Write([]byte(`<SendMessageResponse><SendMessageResult/></SendMessageResponse>`))
			}
		default:
			t.Error("unexpected SQS action")
		}
	}))
	defer server.Close()
	endpoint = server.URL
	queue := "synthetic-events.fifo"
	journal, err := newUserJournal(context.Background(), &brigAWSConfig{UserJournalQueue: &queue, SQSEndpoint: endpoint + "/ignored/path"}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.enqueue(context.Background(), []byte("synthetic")); err != nil || sent != 2 {
		t.Fatal("native throttled FIFO retry failed", err)
	}
	if journal.String() != "<UserJournal>" {
		t.Fatal("journal may reveal credentials")
	}
	if disabled, err := newUserJournal(context.Background(), nil, nil); err != nil || disabled != nil {
		t.Fatal("optional journal became mandatory")
	}
}
