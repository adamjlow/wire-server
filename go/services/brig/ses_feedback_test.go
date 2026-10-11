// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/xml"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
)

func TestSESNativeMailboxGrammar(t *testing.T) {
	for _, tc := range []struct{ input, address string }{
		{"Alice@Example.com", "Alice@Example.com"},
		{"<Alice@Example.com>", "Alice@Example.com"},
		{"John Q. Doe <Alice@Example.com>", "Alice@Example.com"},
		{`"Display \"Name\"" <Alice@Example.com>`, "Alice@Example.com"},
		{"(nested(comment)) John <Alice@Example.com> (tail)", "Alice@Example.com"},
		{"John\r\n Doe <Alice@Example.com>", "Alice@Example.com"},
		{`John <"a>b"@Example.com>`, `"a>b"@Example.com`},
		{"John <a@[x>y]>", "a@[x>y]"},
		{"John <a@Example.com.>", "a@Example.com"},
		{"John <a@example.com> extra", ""},
		{"Group: a@example.com;", ""},
		{"Jöhn <a@example.com>", ""},
		{"John (bad ) <a@example.com>", ""},
		{"John (unclosed <a@example.com>", ""},
		{"John <a@example.com", ""},
	} {
		address, err := sesMailboxAddress(tc.input)
		if address != tc.address || (err == nil) != (tc.address != "") {
			t.Fatal("native mailbox grammar changed", tc.input, address, err)
		}
	}
}

func feedbackBody(kind string) string {
	if kind == "Complaint" {
		return `{"notificationType":"Complaint","complaint":{"complainedRecipients":[{"emailAddress":"Display <Alice@Example.com>"}]}}`
	}
	return `{"notificationType":"Bounce","bounce":{"bounceType":"` + kind + `","bouncedRecipients":[{"emailAddress":"Display <Alice@Example.com>"}]}}`
}

func TestSESFeedbackSchemaAndPolicy(t *testing.T) {
	for _, kind := range []string{"Permanent", "Transient", "Undetermined", "Complaint"} {
		event, err := decodeSESFeedback(feedbackBody(kind))
		if err != nil || event.block != (kind == "Permanent" || kind == "Complaint") || len(event.addresses) != 1 || event.addresses[0] != "Alice@Example.com" {
			t.Fatal("native feedback policy changed", kind, event, err)
		}
	}
	for _, body := range []string{
		`null`, `{}`, `{"notificationType":null}`, `{"notificationType":"Unknown"}`,
		`{"notificationType":"Bounce","bounce":null}`, feedbackBody("Unknown"),
		`{"notificationType":"Complaint","complaint":{"complainedRecipients":null}}`,
		`{"notificationType":"Complaint","complaint":{"complainedRecipients":{}}}`,
		`{"notificationType":"Complaint","complaint":{"complainedRecipients":[null]}}`,
		`{"notificationType":"Complaint","complaint":{"complainedRecipients":[{"emailAddress":null}]}}`,
		feedbackBody("Permanent") + ` {}`, strings.ReplaceAll(feedbackBody("Permanent"), "Alice@Example.com", "not-an-address"),
	} {
		if _, err := decodeSESFeedback(body); err == nil {
			t.Fatal("invalid feedback accepted", body)
		}
	}
	if event, err := decodeSESFeedback(`{"notificationType":"Complaint","complaint":{"complainedRecipients":[]}}`); err != nil || len(event.addresses) != 0 {
		t.Fatal("native empty recipient list rejected")
	}
}

func feedbackTestQueue(t *testing.T, handler http.HandlerFunc) *userJournal {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &userJournal{client: server.Client(), endpoint: server.URL, queue: server.URL + "/synthetic-feedback", region: "us-east-1", now: time.Now, credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
		return aws.Credentials{AccessKeyID: "synthetic-access", SecretAccessKey: "synthetic-secret"}, nil
	})}
}

func TestSESFeedbackAcknowledgementAndFailure(t *testing.T) {
	for _, mode := range []string{"Permanent", "Complaint", "Transient", "Undetermined", "missing-body", "malformed", "no-receipt", "callback-failure", "delete-failure"} {
		t.Run(mode, func(t *testing.T) {
			var deletes atomic.Int32
			queue := feedbackTestQueue(t, func(w http.ResponseWriter, r *http.Request) {
				if r.ParseForm() != nil || r.Form.Get("Action") != "DeleteMessage" || r.Form.Get("ReceiptHandle") != "synthetic-receipt" || r.Form.Get("QueueUrl") == "" || !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
					t.Error("native acknowledgement request changed")
				}
				deletes.Add(1)
				if mode == "delete-failure" {
					w.WriteHeader(503)
					w.Write([]byte(`<ErrorResponse><Error><Code>Unavailable</Code></Error></ErrorResponse>`))
					return
				}
				w.Write([]byte(`<DeleteMessageResponse/>`))
			})
			blocks := 0
			listener := &sesFeedbackListener{queue: queue, log: func(string) {}, block: func(_ context.Context, address string) error {
				blocks++
				if address != "Alice@Example.com" {
					t.Fatal("mailbox address not canonicalized")
				}
				if mode == "callback-failure" {
					return errors.New("synthetic store failure")
				}
				return nil
			}}
			kind := mode
			if mode != "Complaint" && mode != "Transient" && mode != "Undetermined" {
				kind = "Permanent"
			}
			body, receipt := feedbackBody(kind), "synthetic-receipt"
			message := sqsFeedbackMessage{Body: &body, Receipt: &receipt}
			if mode == "missing-body" {
				message.Body = nil
			}
			if mode == "malformed" {
				body = "not JSON"
			}
			if mode == "no-receipt" {
				message.Receipt = nil
			}
			err := listener.message(context.Background(), message)
			failed := mode == "callback-failure" || mode == "delete-failure"
			ack := mode != "missing-body" && mode != "malformed" && mode != "no-receipt" && mode != "callback-failure"
			block := mode != "missing-body" && mode != "malformed" && mode != "Transient" && mode != "Undetermined"
			if (err != nil) != failed || (deletes.Load() == 1) != ack || (blocks == 1) != block {
				t.Fatal("native callback/acknowledgement policy changed", err, deletes.Load(), blocks)
			}
		})
	}
}

func TestSESFeedbackBatchFailureCancelsAndJoinsSiblings(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var started, finished atomic.Int32
	allStarted := make(chan struct{})
	failure := errors.New("synthetic callback failure")
	l := &sesFeedbackListener{log: func(string) {}, block: func(ctx context.Context, _ string) error {
		n := started.Add(1)
		defer finished.Add(1)
		if n == 10 {
			close(allStarted)
		}
		select {
		case <-allStarted:
		case <-ctx.Done():
			return ctx.Err()
		}
		if n == 1 {
			return failure
		}
		<-ctx.Done()
		return ctx.Err()
	}}
	body := feedbackBody("Permanent")
	messages := make([]sqsFeedbackMessage, 10)
	for i := range messages {
		messages[i].Body = &body
	}
	if err := l.process(ctx, messages); !errors.Is(err, failure) || started.Load() != 10 || finished.Load() != 10 {
		t.Fatal("native concurrent callback failure did not cancel and join siblings", err, started.Load(), finished.Load())
	}
}

func TestSESFeedbackReceiveAndCancellation(t *testing.T) {
	requests := make(chan struct{}, 1)
	queue := feedbackTestQueue(t, func(w http.ResponseWriter, r *http.Request) {
		if r.ParseForm() != nil || r.Form.Get("Action") != "ReceiveMessage" || r.Form.Get("WaitTimeSeconds") != "20" || r.Form.Get("MaxNumberOfMessages") != "10" || r.Form.Get("QueueUrl") == "" {
			t.Error("native receive limits changed")
		}
		requests <- struct{}{}
		<-r.Context().Done()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	l := &sesFeedbackListener{queue: queue, ctx: ctx, cancel: cancel, done: make(chan struct{}), log: func(string) {}}
	defer l.close()
	go func() { defer close(l.done); l.listen() }()
	select {
	case <-requests:
	case <-time.After(2 * time.Second):
		t.Fatal("feedback listener did not receive")
	}
	l.close()
	var result journalResponse
	if xml.Unmarshal([]byte(`<ReceiveMessageResponse><ReceiveMessageResult><Message><Body>synthetic</Body></Message></ReceiveMessageResult></ReceiveMessageResponse>`), &result) != nil || len(result.Messages) != 1 || result.Messages[0].Receipt != nil || result.Messages[0].Body == nil {
		t.Fatal("native optional message fields changed")
	}
	if startSESFeedback(context.Background(), nil, blocklistStore{}, nil) != nil {
		t.Fatal("SMTP enabled SES feedback")
	}
}
