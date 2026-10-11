// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/mail"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
)

func TestSESMailActualSignedDeliveryAndStableRetry(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "synthetic-access")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "synthetic-secret")
	t.Setenv("AWS_SESSION_TOKEN", "synthetic-session")
	t.Setenv("AWS_REGION", "eu-west-1")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	calls := 0
	previous := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "POST" || r.URL.Path != "/" || r.URL.RawQuery != "" {
			t.Error("native endpoint/method changed")
		}
		if !strings.Contains(r.Header.Get("Authorization"), "/eu-west-1/ses/aws4_request") || r.Header.Get("X-Amz-Security-Token") != "synthetic-session" {
			t.Error("SES scope/session signing missing")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		if previous != "" && previous != string(body) {
			t.Error("SES retry changed MIME or payload")
		}
		previous = string(body)
		r.Body = io.NopCloser(bytes.NewReader(body))
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.Form.Get("Action") != "SendRawEmail" || r.Form.Get("Version") != "2010-12-01" || r.Form.Get("Source") != "sender@example.com" || r.Form.Get("Destinations.member.1") != "recipient@example.com" {
			t.Error("native raw mail query changed")
		}
		raw, err := base64.StdEncoding.DecodeString(r.Form.Get("RawMessage.Data"))
		if err != nil {
			t.Error(err)
		}
		message, err := mail.ReadMessage(bytes.NewReader(raw))
		if err != nil {
			t.Error(err)
		} else if message.Header.Get("X-Zeta-Purpose") != "Activation" || message.Header.Get("X-Zeta-Key") != "synthetic-key" || message.Header.Get("X-Zeta-Code") != "123456" {
			t.Error("activation headers lost in delivery")
		}
		w.Header().Set("Content-Type", "text/xml")
		if calls == 1 {
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`<ErrorResponse><Error><Code>RequestThrottled</Code><Message>synthetic</Message></Error></ErrorResponse>`))
			return
		}
		_, _ = w.Write([]byte(`<SendRawEmailResponse xmlns="http://ses.amazonaws.com/doc/2010-12-01/"><SendRawEmailResult><MessageId>synthetic-message</MessageId></SendRawEmailResult></SendRawEmailResponse>`))
	}))
	defer server.Close()
	config, err := parseSESMailConfig(map[string]any{"sesEndpoint": server.URL + "/ignored?discard=true", "sesQueue": "synthetic-feedback"})
	if err != nil {
		t.Fatal(err)
	}
	sender, err := newSESMailSender(context.Background(), config, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	message := brigMail{from: "sender@example.com", fromName: "Synthetic", to: "recipient@example.com", subject: "Synthetic activation", plain: "synthetic", html: "<p>synthetic</p>", headers: [][2]string{{"X-Zeta-Purpose", "Activation"}, {"X-Zeta-Key", "synthetic-key"}, {"X-Zeta-Code", "123456"}}}
	if err := sender.send(context.Background(), message); err != nil || calls != 2 {
		t.Fatalf("actual signed mail/retry failed: %v calls=%d", err, calls)
	}
	if sender.String() != "<SESMailSender>" || config.String() != "<SESMailConfig>" {
		t.Fatal("mail credentials/config not redacted")
	}
}

type sesTestRoundTrip func(*http.Request) (*http.Response, error)

func (f sesTestRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type sesTestTimeout struct{}

func (sesTestTimeout) Error() string   { return "synthetic timeout" }
func (sesTestTimeout) Timeout() bool   { return true }
func (sesTestTimeout) Temporary() bool { return true }

func TestSESMailResponseRetryAndErrorContracts(t *testing.T) {
	for _, test := range []struct {
		name, body                               string
		status, calls                            int
		write, timeout, transportFailure, cancel bool
		invalidDomain, success                   bool
	}{
		{name: "success", status: 200, body: `<SendRawEmailResponse><SendRawEmailResult><MessageId>synthetic</MessageId></SendRawEmailResult></SendRawEmailResponse>`, calls: 1, success: true},
		{name: "empty-required-text", status: 200, body: `<SendRawEmailResponse><SendRawEmailResult><MessageId/></SendRawEmailResult></SendRawEmailResponse>`, calls: 1, success: true},
		{name: "missing-message-id", status: 200, body: `<SendRawEmailResponse><SendRawEmailResult/></SendRawEmailResponse>`, calls: 1},
		{name: "wrong-response", status: 200, body: `<Unrelated/>`, calls: 1},
		{name: "malformed", status: 200, body: `<broken`, calls: 1},
		{name: "domain", status: 400, body: `<ErrorResponse><Error><Code>InvalidParameterValue</Code><Message>Invalid domain name: synthetic</Message></Error></ErrorResponse>`, calls: 1, invalidDomain: true},
		{name: "prefix-case", status: 400, body: `<ErrorResponse><Error><Message>invalid domain name</Message></Error></ErrorResponse>`, calls: 1},
		{name: "domain-500", status: 500, body: `<ErrorResponse><Error><Message>Invalid domain name: synthetic</Message></Error></ErrorResponse>`, calls: 1},
		{name: "no-blanket-500-retry", status: 500, body: `<ErrorResponse><Error><Code>InternalFailure</Code></Error></ErrorResponse>`, calls: 1},
		{name: "no-new-throttle-code", status: 429, body: `<ErrorResponse><Error><Code>Throttling</Code></Error></ErrorResponse>`, calls: 1},
		{name: "native-throttle-budget", status: 400, body: `<ErrorResponse><Error><Code>RequestThrottled</Code></Error></ErrorResponse>`, calls: 6},
		{name: "response-timeout-budget", timeout: true, write: true, calls: 6},
		{name: "connection-timeout-no-retry", timeout: true, calls: 1},
		{name: "connection-failure-no-retry", transportFailure: true, write: true, calls: 1},
		{name: "cancellation-no-retry", timeout: true, write: true, cancel: true, calls: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			var sleeps []time.Duration
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			sender := sesMailSender{config: sesMailConfig{endpoint: "http://synthetic.invalid/"}, region: "eu-west-1", now: time.Now, credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
				return aws.Credentials{AccessKeyID: "synthetic", SecretAccessKey: "synthetic"}, nil
			}), wait: func(ctx context.Context, d time.Duration) error { sleeps = append(sleeps, d); return ctx.Err() }}
			sender.client = &http.Client{Transport: sesTestRoundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				if test.write {
					httptrace.ContextClientTrace(r.Context()).WroteRequest(httptrace.WroteRequestInfo{})
				}
				if test.cancel {
					cancel()
				}
				if test.timeout {
					return nil, sesTestTimeout{}
				}
				if test.transportFailure {
					return nil, errors.New("synthetic private details")
				}
				return &http.Response{StatusCode: test.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(test.body)), Request: r}, nil
			})}
			err := sender.send(ctx, brigMail{from: "sender@example.com", to: "recipient@example.com"})
			if (err == nil) != test.success || errors.Is(err, errSESMailInvalidDomain) != test.invalidDomain || calls != test.calls {
				t.Fatalf("wrong error/retry: %v calls=%d", err, calls)
			}
			if calls == 6 && !reflect.DeepEqual(sleeps, []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond, 800 * time.Millisecond, 1600 * time.Millisecond}) {
				t.Fatalf("native retry budget/backoff changed: %v", sleeps)
			}
			if err != nil && strings.Contains(err.Error(), "private details") {
				t.Fatal("transport details leaked")
			}
		})
	}
	recorder := httptest.NewRecorder()
	registrationHTTPError(recorder, errSESMailInvalidDomain)
	if recorder.Code != 400 || !strings.Contains(recorder.Body.String(), `"label":"invalid-email"`) || !strings.Contains(recorder.Body.String(), "Invalid e-mail address.") {
		t.Fatal("native SES invalid-email mapping changed")
	}
}

func TestSESMailConfigRequiredShape(t *testing.T) {
	for _, raw := range []any{nil, map[string]any{"sesEndpoint": "http://example.com"}, map[string]any{"sesQueue": "q", "sesEndpoint": "ftp://example.com"}, map[string]any{"sesQueue": 4, "sesEndpoint": "http://example.com"}, map[string]any{"sesQueue": "q", "sesEndpoint": "http://"}} {
		if _, err := parseSESMailConfig(raw); err == nil {
			t.Fatal("invalid SES shape accepted")
		}
	}
	// Native Text fields permit an empty queue; its actual use remains feedback startup.
	if _, err := parseSESMailConfig(map[string]any{"sesQueue": "", "sesEndpoint": "https://example.com/path"}); err != nil {
		t.Fatal(err)
	}
	if err := (*sesMailSender)(nil).send(context.Background(), brigMail{}); err == nil {
		t.Fatal("missing mail transport returned success")
	}
}
