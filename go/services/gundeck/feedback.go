package gundeck

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/service/sns"
)

type feedbackEvent struct {
	ARN     string `json:"EndpointArn"`
	Type    string `json:"EventType"`
	Failure string `json:"FailureType"`
}
type sqsMessage struct {
	Body    string `xml:"Body"`
	Receipt string `xml:"ReceiptHandle"`
}
type sqsResponse struct {
	QueueURL string       `xml:"GetQueueUrlResult>QueueUrl"`
	Messages []sqsMessage `xml:"ReceiveMessageResult>Message"`
}

func parseFeedback(body string) (feedbackEvent, error) {
	var event feedbackEvent
	if err := json.Unmarshal([]byte(body), &event); err != nil || event.Type == "" {
		return event, fmt.Errorf("invalid feedback event")
	}
	parts := strings.SplitN(event.ARN, ":", 6)
	if len(parts) != 6 || parts[0] != "arn" || parts[1] != "aws" || parts[2] != "sns" || parts[3] == "" {
		return event, fmt.Errorf("invalid feedback ARN")
	}
	topic := strings.Split(parts[5], "/")
	if len(topic) != 4 || topic[0] != "endpoint" || transportCode(topic[1]) < 0 || topic[2] == "" || topic[3] == "" {
		return event, fmt.Errorf("invalid feedback endpoint")
	}
	return event, nil
}

// SQS Query/XML is the current backend wire protocol. Credentials and SigV4
// come from the maintained AWS SDK; there is no custom credential or crypto code.
func (s *Server) sqsQuery(ctx context.Context, endpoint, action string, params url.Values) (sqsResponse, error) {
	var result sqsResponse
	params.Set("Action", action)
	params.Set("Version", "2012-11-05")
	body := params.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(body))
	if err != nil {
		return result, fmt.Errorf("invalid SQS endpoint")
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	credentials, err := s.aws.Credentials.Retrieve(ctx)
	if err != nil {
		return result, fmt.Errorf("AWS credentials unavailable")
	}
	sum := sha256.Sum256([]byte(body))
	if err = v4.NewSigner().SignHTTP(ctx, credentials, request, hex.EncodeToString(sum[:]), "sqs", s.Config.AWS.Region, time.Now()); err != nil {
		return result, fmt.Errorf("SQS signing failed")
	}
	response, err := s.awsHTTP.Do(request)
	if err != nil {
		return result, fmt.Errorf("SQS request failed")
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4*1024*1024+1))
	if err != nil || len(raw) > 4*1024*1024 {
		return result, fmt.Errorf("SQS response exceeds bound")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return result, fmt.Errorf("SQS request rejected")
	}
	if err = xml.Unmarshal(raw, &result); err != nil {
		return result, fmt.Errorf("invalid SQS response")
	}
	return result, nil
}
func (s *Server) openFeedback(ctx context.Context) error {
	endpoint := s.Config.AWS.SQS
	if endpoint == "" {
		endpoint = "https://sqs." + s.Config.AWS.Region + ".amazonaws.com"
	}
	result, err := s.sqsQuery(ctx, endpoint, "GetQueueUrl", url.Values{"QueueName": {s.Config.AWS.Queue}})
	if err != nil {
		return err
	}
	parsed, err := url.Parse(result.QueueURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("invalid SQS queue URL")
	}
	s.queueURL = result.QueueURL
	return nil
}
func waitContext(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}
func (s *Server) listenFeedback() {
	throttle := time.Second
	if s.Config.Settings.SQSThrottle != nil {
		throttle = time.Duration(*s.Config.Settings.SQSThrottle) * time.Millisecond
	}
	for s.background.Err() == nil {
		result, err := s.sqsQuery(s.background, s.queueURL, "ReceiveMessage", url.Values{"MaxNumberOfMessages": {"10"}, "WaitTimeSeconds": {"20"}})
		if err != nil {
			s.logger.Event("Error", "SQS receive failed")
			if !waitContext(s.background, 3*time.Second) {
				return
			}
			continue
		}
		failed := false
		var failureMu sync.Mutex
		_ = parallel(len(result.Messages), 10, func(i int) error {
			err := s.processFeedback(s.background, result.Messages[i], s.onFeedback)
			if err != nil {
				failureMu.Lock()
				failed = true
				failureMu.Unlock()
				s.logger.Event("Error", "SQS feedback callback failed")
			}
			return nil
		})
		if failed {
			if !waitContext(s.background, 3*time.Second) {
				return
			}
		} else if len(result.Messages) == 0 {
			if !waitContext(s.background, throttle) {
				return
			}
		}
	}
}
func (s *Server) onFeedback(ctx context.Context, event feedbackEvent) error {
	if event.Type != "EndpointUpdated" && !(event.Type == "DeliveryFailure" && (event.Failure == "InvalidPlatformToken" || event.Failure == "DeliveryFailedPerm" || event.Failure == "EndpointDisabled")) {
		return nil
	}
	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()
	attrs, err := s.endpoint(ctx, event.ARN)
	if err != nil || attrs == nil {
		return err
	}
	addresses := []pushAddress{}
	for _, user := range endpointUsers(attrs) {
		tokens, err := s.lookupTokens(ctx, user, false)
		if err != nil {
			return err
		}
		for _, address := range tokens {
			if address.ARN == event.ARN {
				addresses = append(addresses, address)
			}
		}
	}
	erase := len(addresses) == 0
	if len(addresses) > 0 {
		permanent := event.Type == "DeliveryFailure" && (event.Failure == "InvalidPlatformToken" || event.Failure == "DeliveryFailedPerm")
		disabled := attrs["Enabled"] != "true"
		if event.Type == "DeliveryFailure" && event.Failure == "EndpointDisabled" && !disabled {
			return nil
		}
		kept := []string{}
		removed := false
		for _, address := range addresses {
			if permanent || disabled || (event.Type == "EndpointUpdated" && address.Token != attrs["Token"]) {
				if err = s.tokenRemoved(ctx, address, true); err != nil {
					return err
				}
				if err = s.deleteTokenRow(ctx, address); err != nil {
					return err
				}
				removed = true
			} else {
				kept = append(kept, address.User)
			}
		}
		erase = len(kept) == 0
		if !erase && removed {
			address := addresses[0]
			address.Token = attrs["Token"]
			if err = s.updateEndpoint(ctx, address, kept); err != nil {
				return err
			}
		}
	}
	if erase {
		_, err = s.sns.DeleteEndpoint(ctx, &sns.DeleteEndpointInput{EndpointArn: aws.String(event.ARN)})
	}
	return err
}

func (s *Server) processFeedback(ctx context.Context, message sqsMessage, callback func(context.Context, feedbackEvent) error) error {
	event, err := parseFeedback(message.Body)
	if err != nil {
		s.logger.Event("Error", "SQS feedback is malformed")
		return nil
	}
	if err = callback(ctx, event); err != nil {
		return err
	}
	if message.Receipt != "" {
		_, err = s.sqsQuery(ctx, s.queueURL, "DeleteMessage", url.Values{"ReceiptHandle": {message.Receipt}})
	}
	return err
}
