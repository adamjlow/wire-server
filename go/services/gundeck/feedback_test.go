package gundeck

import (
	"context"
	"errors"
	"github.com/aws/aws-sdk-go-v2/aws"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFeedbackAcknowledgesOnlySuccessfulValidCallbacks(t *testing.T) {
	deleted := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
			t.Error("unsigned SQS request")
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.Form.Get("Action") != "DeleteMessage" || r.Form.Get("ReceiptHandle") != "owned-receipt" {
			t.Error("unexpected SQS operation")
		}
		deleted++
		_, _ = w.Write([]byte(`<DeleteMessageResponse><DeleteMessageResult/></DeleteMessageResponse>`))
	}))
	defer provider.Close()
	s := &Server{awsHTTP: provider.Client(), queueURL: provider.URL}
	s.Config.AWS.Region = "eu-west-1"
	s.aws.Credentials = aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
		return aws.Credentials{AccessKeyID: "SYNTHETIC", SecretAccessKey: "synthetic-fixture"}, nil
	})
	message := sqsMessage{Body: `{"EndpointArn":"arn:aws:sns:eu-west-1:123456789012:endpoint/GCM/test-app/device","EventType":"DeliveryFailure","FailureType":"NotificationTTLExpired"}`, Receipt: "owned-receipt"}
	calls := 0
	failed := func(context.Context, feedbackEvent) error { calls++; return errors.New("callback failed") }
	if err := s.processFeedback(context.Background(), message, failed); err == nil || deleted != 0 || calls != 1 {
		t.Fatal("failed callback acknowledged")
	}
	malformed := message
	malformed.Body = `{"EventType":"DeliveryFailure"}`
	if err := s.processFeedback(context.Background(), malformed, failed); err != nil || deleted != 0 || calls != 1 {
		t.Fatal("malformed event callback/acknowledgement")
	}
	if err := s.processFeedback(context.Background(), message, func(context.Context, feedbackEvent) error { calls++; return nil }); err != nil || deleted != 1 || calls != 2 {
		t.Fatal("successful event was not acknowledged", err)
	}
}
