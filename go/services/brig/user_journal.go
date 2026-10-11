// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
)

type brigAWSConfig struct {
	UserJournalQueue *string `yaml:"userJournalQueue"`
	SQSEndpoint      string  `yaml:"sqsEndpoint"`
	PrekeyTable      string  `yaml:"prekeyTable"`
	DynamoEndpoint   *string `yaml:"dynamoDBEndpoint"`
}

type userJournal struct {
	client                  *http.Client
	credentials             aws.CredentialsProvider
	region, endpoint, queue string
	now                     func() time.Time
}

func (userJournal) String() string { return "<UserJournal>" }

type journalResponse struct {
	Queue    string               `xml:"GetQueueUrlResult>QueueUrl"`
	MD5      *string              `xml:"SendMessageResult>MD5OfMessageBody"`
	Messages []sqsFeedbackMessage `xml:"ReceiveMessageResult>Message"`
	Error    struct {
		Code string `xml:"Code"`
	} `xml:"Error"`
}

func newUserJournal(ctx context.Context, c *brigAWSConfig, client *http.Client) (*userJournal, error) {
	if c == nil || c.UserJournalQueue == nil {
		return nil, nil
	}
	return newBrigSQS(ctx, c, *c.UserJournalQueue, client)
}

func newBrigSQS(ctx context.Context, c *brigAWSConfig, name string, client *http.Client) (*userJournal, error) {
	if c == nil {
		return nil, errors.New("queue AWS configuration unavailable")
	}
	parsed, err := url.Parse(c.SQSEndpoint)
	if err != nil || parsed.Hostname() == "" || parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("invalid journal endpoint")
	}
	// Native AWSEndpoint discards URL path/query, retaining scheme/host/port.
	endpoint := (&url.URL{Scheme: parsed.Scheme, Host: parsed.Host, Path: "/"}).String()
	config, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithDefaultRegion("us-east-1"), awsconfig.WithHTTPClient(client))
	if err != nil {
		return nil, errors.New("journal credentials unavailable")
	}
	j := &userJournal{client: client, credentials: config.Credentials, region: config.Region, endpoint: endpoint, now: time.Now}
	result, _, err := j.query(ctx, "GetQueueUrl", url.Values{"QueueName": {name}})
	if err != nil {
		return nil, err
	}
	queue, err := url.Parse(result.Queue)
	if err != nil || queue.Hostname() == "" || queue.Scheme != "http" && queue.Scheme != "https" {
		return nil, errors.New("invalid journal queue URL")
	}
	j.queue = result.Queue
	return j, nil
}
func (j *userJournal) query(ctx context.Context, action string, params url.Values) (journalResponse, bool, error) {
	var result journalResponse
	params.Set("Action", action)
	params.Set("Version", "2012-11-05")
	body := params.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, j.endpoint, strings.NewReader(body))
	if err != nil {
		return result, false, errors.New("invalid journal request")
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	credentials, err := j.credentials.Retrieve(ctx)
	if err != nil {
		return result, false, errors.New("journal credentials unavailable")
	}
	sum := sha256.Sum256([]byte(body))
	if v4.NewSigner().SignHTTP(ctx, credentials, request, hex.EncodeToString(sum[:]), "sqs", j.region, j.now()) != nil {
		return result, false, errors.New("journal signing failed")
	}
	response, err := j.client.Do(request)
	if err != nil {
		var network net.Error
		return result, errors.As(err, &network) && network.Timeout() && ctx.Err() == nil, errors.New("journal request failed")
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4<<20+1))
	if err != nil || len(raw) > 4<<20 {
		return result, false, errors.New("invalid journal response")
	}
	if xml.Unmarshal(raw, &result) != nil {
		return result, false, errors.New("invalid journal response")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return result, result.Error.Code == "RequestThrottled", errors.New("journal request rejected")
	}
	return result, false, nil
}

// Encode the native proto2 UserEvent fields directly, without JSON conversion
// or another protobuf runtime. Required zero-valued enum fields are present.
func encodeUserJournal(event uint64, user gocql.UUID, at time.Time, email, locale, name *string, team *gocql.UUID) []byte {
	raw := binary.AppendUvarint(nil, 8)
	raw = binary.AppendUvarint(raw, event)
	bytesField := func(field uint64, value []byte) {
		raw = binary.AppendUvarint(raw, field<<3|2)
		raw = binary.AppendUvarint(raw, uint64(len(value)))
		raw = append(raw, value...)
	}
	bytesField(2, user[:])
	seconds := at.Unix()
	if at.Nanosecond() > 500000000 || at.Nanosecond() == 500000000 && seconds%2 != 0 {
		seconds++
	}
	raw = binary.AppendUvarint(raw, 24)
	raw = binary.AppendUvarint(raw, uint64(seconds))
	if email != nil {
		bytesField(4, []byte(*email))
	}
	if locale != nil {
		bytesField(5, []byte(*locale))
	}
	if team != nil {
		bytesField(6, team[:])
	}
	if name != nil {
		bytesField(7, []byte(*name))
	}
	return raw
}
func (j *userJournal) enqueue(ctx context.Context, event []byte) error {
	if j == nil {
		return nil
	} // Native journaling is optional.
	dedup, err := gocql.RandomUUID()
	if err != nil {
		return errors.New("journal randomness unavailable")
	}
	params := url.Values{"QueueUrl": {j.queue}, "MessageBody": {base64.StdEncoding.EncodeToString(event)}, "MessageGroupId": {"user.events"}, "MessageDeduplicationId": {dedup.String()}}
	for attempt := 0; ; attempt++ {
		_, retry, err := j.query(ctx, "SendMessage", params)
		if err == nil {
			return nil
		}
		if !retry || attempt >= 5 || ctx.Err() != nil {
			return err
		}
		if waitBrigRetry(ctx, 100*time.Millisecond<<attempt) != nil {
			return errors.New("journal request canceled")
		}
	}
}
