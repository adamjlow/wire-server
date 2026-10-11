// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
)

var errSESMailInvalidDomain = errors.New("mail recipient domain rejected")

type sesMailConfig struct{ endpoint, feedbackQueue string }

func (sesMailConfig) String() string { return "<SESMailConfig>" }

// Native EmailOpts tries the complete SES shape first, then SMTP if parsing
// SES fails. Keep this parser separate so SMTP fallback can preserve that rule.
func parseSESMailConfig(raw any) (sesMailConfig, error) {
	var result sesMailConfig
	failure := errors.New("invalid SES mail configuration")
	fields, ok := raw.(map[string]any)
	if !ok {
		return result, failure
	}
	endpoint, ok := fields["sesEndpoint"].(string)
	if !ok {
		return result, failure
	}
	queue, ok := fields["sesQueue"].(string)
	if !ok {
		return result, failure
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return result, failure
	}
	// Native AWSEndpoint retains scheme/host/port and discards URL path/query.
	result.endpoint = (&url.URL{Scheme: parsed.Scheme, Host: parsed.Host, Path: "/"}).String()
	result.feedbackQueue = queue
	return result, nil
}

type sesMailSender struct {
	config      sesMailConfig
	client      *http.Client
	credentials aws.CredentialsProvider
	region      string
	now         func() time.Time
	wait        func(context.Context, time.Duration) error
}

func (*sesMailSender) String() string { return "<SESMailSender>" }

func newSESMailSender(ctx context.Context, c sesMailConfig, client *http.Client) (*sesMailSender, error) {
	if client == nil || c.endpoint == "" {
		return nil, errors.New("mail delivery unavailable")
	}
	config, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithDefaultRegion("us-east-1"), awsconfig.WithHTTPClient(client))
	if err != nil || config.Credentials == nil {
		return nil, errors.New("mail credentials unavailable")
	}
	return &sesMailSender{config: c, client: client, credentials: config.Credentials, region: config.Region, now: time.Now, wait: waitBrigRetry}, nil
}

func (s *sesMailSender) send(ctx context.Context, mail brigMail) error {
	if s == nil || s.client == nil || s.credentials == nil || s.now == nil || s.wait == nil {
		return errors.New("mail delivery unavailable")
	}
	raw, err := mail.mime()
	if err != nil {
		return err
	}
	params := url.Values{"Action": {"SendRawEmail"}, "Version": {"2010-12-01"}, "Source": {mail.from}, "Destinations.member.1": {mail.to}, "RawMessage.Data": {base64.StdEncoding.EncodeToString(raw)}}
	// Render once, retaining identical MIME/body data across native five retries.
	body := params.Encode()
	for attempt := 0; ; attempt++ {
		retry, err := s.query(ctx, body)
		if err == nil {
			return nil
		}
		if !retry || attempt >= 5 || ctx.Err() != nil {
			return err
		}
		if s.wait(ctx, 100*time.Millisecond<<attempt) != nil {
			return errors.New("mail delivery canceled")
		}
	}
}

func (s *sesMailSender) query(ctx context.Context, body string) (bool, error) {
	failure := errors.New("mail delivery failed")
	var written atomic.Bool
	trace := &httptrace.ClientTrace{WroteRequest: func(info httptrace.WroteRequestInfo) {
		if info.Err == nil {
			written.Store(true)
		}
	}}
	request, err := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, trace), http.MethodPost, s.config.endpoint, strings.NewReader(body))
	if err != nil {
		return false, failure
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	credentials, err := s.credentials.Retrieve(ctx)
	if err != nil {
		return false, errors.New("mail credentials unavailable")
	}
	sum := sha256.Sum256([]byte(body))
	if v4.NewSigner().SignHTTP(ctx, credentials, request, hex.EncodeToString(sum[:]), "ses", s.region, s.now()) != nil {
		return false, errors.New("mail signing failed")
	}
	// Native retries response timeouts, not dial/TLS/authentication failures.
	timeout := func(err error) bool {
		var network net.Error
		return written.Load() && ctx.Err() == nil && errors.As(err, &network) && network.Timeout()
	}
	response, err := s.client.Do(request)
	if err != nil {
		return timeout(err), failure
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4<<20+1))
	if err != nil {
		return timeout(err), failure
	}
	if len(raw) > 4<<20 {
		return false, failure
	}
	var result struct {
		XMLName   xml.Name
		MessageID *string                        `xml:"SendRawEmailResult>MessageId"`
		Error     struct{ Code, Message string } `xml:"Error"`
	}
	if xml.Unmarshal(raw, &result) != nil {
		return false, failure
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		rejected := failure
		if response.StatusCode == 400 && strings.HasPrefix(result.Error.Message, "Invalid domain name") {
			rejected = errSESMailInvalidDomain
		}
		return result.Error.Code == "RequestThrottled", rejected
	}
	// Amazonka's SendRawEmailResponse requires MessageId (including empty Text).
	if result.XMLName.Local != "SendRawEmailResponse" || result.MessageID == nil {
		return false, failure
	}
	return false, nil
}
