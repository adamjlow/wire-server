// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func nativeMailTestConfig(t *testing.T) Config {
	t.Helper()
	c, err := LoadConfig("../../../services/brig/brig.integration.yaml")
	if err != nil {
		t.Fatal(err)
	}
	c.Other["emailSMS"].(map[string]any)["general"].(map[string]any)["templateDir"] = "../../../libs/wire-subsystems/templates"
	return c
}
func TestRegistrationMailSESFirstActualCallbacksAndFeedbackDiscovery(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "synthetic-access")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "synthetic-secret")
	t.Setenv("AWS_REGION", "eu-west-1")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	var mu sync.Mutex
	var purposes []string
	queues := 0
	var endpoint string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		switch r.Form.Get("Action") {
		case "GetQueueUrl":
			mu.Lock()
			queues++
			mu.Unlock()
			if r.Form.Get("QueueName") != "synthetic-feedback" {
				t.Error("SES startup queue discovery changed")
			}
			_, _ = io.WriteString(w, `<GetQueueUrlResponse><GetQueueUrlResult><QueueUrl>`+endpoint+`/queue</QueueUrl></GetQueueUrlResult></GetQueueUrlResponse>`)
		case "SendRawEmail":
			if r.Form.Get("Source") != "backend-integration@wire.com" || r.Form.Get("Destinations.member.1") != "synthetic@example.com" {
				t.Error("registration mail envelope changed")
			}
			raw, err := base64.StdEncoding.DecodeString(r.Form.Get("RawMessage.Data"))
			if err != nil {
				t.Error(err)
			}
			message, err := mail.ReadMessage(bytes.NewReader(raw))
			if err != nil {
				t.Error(err)
			} else {
				mu.Lock()
				purposes = append(purposes, message.Header.Get("X-Zeta-Purpose"))
				mu.Unlock()
			}
			_, _ = io.WriteString(w, `<SendRawEmailResponse><SendRawEmailResult><MessageId>synthetic</MessageId></SendRawEmailResult></SendRawEmailResponse>`)
		default:
			t.Error("unexpected mail factory action")
			w.WriteHeader(400)
		}
	}))
	defer server.Close()
	endpoint = server.URL
	c := nativeMailTestConfig(t)
	// Complete both shapes: native chooses SES and never reads this bad SMTP secret.
	c.Other["emailSMS"].(map[string]any)["email"] = map[string]any{"sesEndpoint": endpoint, "sesQueue": "synthetic-feedback", "smtpEndpoint": map[string]any{"host": "127.0.0.1", "port": 1}, "smtpConnType": "plain", "smtpCredentials": map[string]any{"smtpUsername": "synthetic", "smtpPassword": "synthetic-nonexistent-file"}}
	c.AWS = &brigAWSConfig{SQSEndpoint: endpoint}
	delivery, err := newRegistrationMailDelivery(context.Background(), c, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer delivery.close()
	email := "synthetic@example.com"
	profile := selfProfile{Email: &email, Name: "Synthetic", Locale: "en"}
	created := registrationCreation{profile: profile, activation: &activationRecord{key: "synthetic-key", code: "123456"}}
	if err := delivery.activation(context.Background(), created, registrationOrigin{}); err != nil {
		t.Fatal(err)
	}
	team, err := gocql.ParseUUID("10000000-0000-0000-0000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	if err := delivery.welcome(context.Background(), profile, registrationCreatedTeam{ID: team, Name: "Synthetic team"}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if queues != 1 || !reflect.DeepEqual(purposes, []string{"Activation", "Welcome"}) {
		t.Fatalf("discovery/actual callbacks mismatch: queues=%d purposes=%v", queues, purposes)
	}
	if delivery.feedback == nil || delivery.feedback.queue != endpoint+"/queue" {
		t.Fatal("actual feedback queue not retained")
	}
}

func TestRegistrationMailInvalidSESFallsBackToSMTP(t *testing.T) {
	f := newSMTPFixture(t, "plain")
	c := nativeMailTestConfig(t)
	config := f.config()
	// Missing required sesQueue makes this an SMTP config despite sesEndpoint.
	c.Other["emailSMS"].(map[string]any)["email"] = map[string]any{"sesEndpoint": "http://synthetic.invalid", "smtpEndpoint": map[string]any{"host": config.endpoint.Host, "port": config.endpoint.Port}, "smtpConnType": "plain"}
	delivery, err := newRegistrationMailDelivery(context.Background(), c, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer delivery.close()
	email := "synthetic@example.com"
	created := registrationCreation{profile: selfProfile{Email: &email, Name: "Synthetic", Locale: "en"}, activation: &activationRecord{key: "synthetic-key", code: "123456"}}
	if err := delivery.activation(context.Background(), created, registrationOrigin{}); err != nil {
		t.Fatal(err)
	}
	body := <-f.received
	if !strings.Contains(body, "X-Zeta-Purpose: Activation") || !strings.Contains(body, "X-Zeta-Code: 123456") {
		t.Fatal("SMTP registration callback did not deliver activation")
	}
	if delivery.feedback != nil {
		t.Fatal("SMTP unexpectedly initialized SES feedback")
	}
	c.Other["emailSMS"].(map[string]any)["email"] = map[string]any{"sesEndpoint": "ftp://synthetic.invalid"}
	if d, err := newRegistrationMailDelivery(context.Background(), c, nil); err == nil {
		d.close()
		t.Fatal("invalid complete transport config accepted")
	}
	if (*registrationMailDelivery)(nil).activation(context.Background(), created, registrationOrigin{}) == nil {
		t.Fatal("missing delivery returned success")
	}
}
func TestSMTPNativeWord16PortNotation(t *testing.T) {
	for _, check := range []struct {
		value any
		want  int
		valid bool
	}{{int(2525), 2525, true}, {float64(2500), 2500, true}, {float64(2.5), 0, false}, {-1, 0, false}, {65536, 0, false}, {"2525", 0, false}, {uint64(65535), 65535, true}} {
		got, ok := smtpNativePort(check.value)
		if ok != check.valid || ok && got != check.want {
			t.Fatal("native Word16 numeric acceptance changed")
		}
	}
}
