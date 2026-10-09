// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"golang.org/x/net/http2"
)

type versionRange struct {
	From  *int `json:"from"`
	Until *int `json:"until_excl"`
}
type notification struct {
	Origin    *string       `json:"ownDomain"`
	Component *string       `json:"targetComponent"`
	Path      *string       `json:"path"`
	Body      *string       `json:"body"`
	Versions  *versionRange `json:"bodyVersions"`
	RequestID *string       `json:"requestId"`
}

func (n notification) valid() bool {
	if n.Origin == nil || n.Component == nil || n.Path == nil || n.Body == nil || !validDomain(*n.Origin) {
		return false
	}
	if *n.Component != "brig" && *n.Component != "galley" && *n.Component != "cargohold" {
		return false
	}
	if n.Versions != nil {
		r := n.Versions
		if r.From == nil || *r.From < 0 || *r.From > 4 || r.Until != nil && (*r.Until < 0 || *r.Until > 4) {
			return false
		}
	}
	return true
}

type payloadBundle struct {
	Notifications []notification `json:"notifications"`
	Policy        *string        `json:"unsupportedVersionPolicy"`
}

type notificationAction int

const (
	notificationACK notificationAction = iota
	notificationReject
	notificationHold
	notificationDrop
)

func notificationClient() *http.Client {
	return &http.Client{Transport: &http2.Transport{AllowHTTP: true, DialTLSContext: func(ctx context.Context, network, address string, _ *tls.Config) (net.Conn, error) {
		return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, address)
	}}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

type federationError struct {
	Code    *int             `json:"code"`
	Label   *string          `json:"label"`
	Message *string          `json:"message"`
	Inner   *federationError `json:"inner"`
}

func (e federationError) valid() bool {
	return e.Code != nil && e.Label != nil && e.Message != nil && (e.Inner == nil || e.Inner.valid())
}

type remoteError struct{ badRequest bool }

func (remoteError) Error() string { return "federation notification request failed" }

func (s *Service) federationRequest(ctx context.Context, client *http.Client, target string, n notification, rpc, component string, version *int, body []byte) ([]byte, error) {
	path := "/rpc/" + url.PathEscape(target) + "/" + url.PathEscape(component) + "/" + url.PathEscape(strings.TrimPrefix(rpc, "/"))
	req, err := http.NewRequestWithContext(ctx, "POST", "http://"+s.Config.Federator.Address()+path, bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("invalid notification request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Wire-Origin-Domain", *n.Origin)
	id := "N/A"
	if n.RequestID != nil {
		id = *n.RequestID
	}
	req.Header.Set("Request-Id", id)
	// The native federation client sends the numeric version header, including
	// its V0 default for the unversioned version-negotiation request.
	v := 0
	if version != nil {
		v = *version
	}
	req.Header.Set("X-Wire-API-Version", strconv.Itoa(v))
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("federation notification transport failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		return data, err
	}
	var e federationError
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	decodeErr := json.Unmarshal(data, &e)
	return nil, remoteError{badRequest: readErr == nil && decodeErr == nil && e.valid() && e.Inner != nil && *e.Inner.Code == 400 && *e.Inner.Label == "bad-request"}
}

func (s *Service) deliverNotification(ctx context.Context, client *http.Client, target string, body []byte) (notificationAction, error) {
	var bundle payloadBundle
	validBundle := json.Unmarshal(body, &bundle) == nil && len(bundle.Notifications) > 0 && (bundle.Policy == nil || *bundle.Policy == "keep_queued" || *bundle.Policy == "drop_if_unsupported")
	for _, n := range bundle.Notifications {
		validBundle = validBundle && n.valid()
	}
	if !validBundle {
		var n notification
		if json.Unmarshal(body, &n) != nil || !n.valid() {
			return notificationReject, nil
		}
		version := 0
		_, err := s.federationRequest(ctx, client, target, n, *n.Path, *n.Component, &version, []byte(*n.Body))
		return notificationACK, err
	}
	first := bundle.Notifications[0]
	data, err := s.federationRequest(ctx, client, target, first, "api-version", "brig", nil, []byte("{}"))
	if err != nil {
		return notificationHold, err
	}
	var info struct {
		Supported *([]int) `json:"supported_versions"`
		Legacy    *([]int) `json:"supported"`
	}
	if json.Unmarshal(data, &info) != nil || info.Legacy == nil {
		return notificationHold, errors.New("invalid federation version response")
	}
	versions := []int{0}
	if info.Supported != nil {
		versions = *info.Supported
	}
	best, index := -1, -1
	for i, n := range bundle.Notifications {
		if n.Versions == nil {
			continue
		}
		for _, v := range versions {
			if v >= 0 && v <= 4 && v >= *n.Versions.From && (n.Versions.Until == nil || v < *n.Versions.Until) && v >= best {
				best, index = v, i
			}
		}
	}
	if index < 0 {
		if bundle.Policy != nil && *bundle.Policy == "drop_if_unsupported" {
			return notificationDrop, nil
		}
		s.metric(target, func(m *notificationMetrics) { m.stuck = true })
		return notificationHold, nil
	}
	n := bundle.Notifications[index]
	_, err = s.federationRequest(ctx, client, target, n, *n.Path, *n.Component, &best, []byte(*n.Body))
	return notificationACK, err
}

func wait(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
func (s *Service) consumeNotifications(ctx context.Context, client *http.Client, target string, deliveries <-chan amqp.Delivery) {
	for {
		var d amqp.Delivery
		select {
		case <-ctx.Done():
			return
		case next, ok := <-deliveries:
			if !ok {
				return
			}
			d = next
		}
		backoff := time.Duration(s.Config.Pusher.Min) * time.Microsecond
		for {
			action, err := s.deliverNotification(ctx, client, target, d.Body)
			if ctx.Err() != nil {
				return
			} // Unsent/uncertain deliveries stay unacknowledged.
			if err != nil {
				var remote remoteError
				drop := errors.As(err, &remote) && remote.badRequest
				s.metric(target, func(m *notificationMetrics) { m.errors++; m.stuck = !drop })
				if drop {
					if d.Reject(false) != nil {
						return
					}
					break
				}
				if !wait(ctx, time.Duration(rand.Int64N(int64(backoff)))) {
					return
				}
				max := time.Duration(s.Config.Pusher.Max) * time.Microsecond
				if backoff < max {
					if backoff > max/2 {
						backoff = max
					} else {
						backoff *= 2
					}
				}
				continue
			}
			switch action {
			case notificationReject:
				if d.Reject(false) != nil {
					return
				}
			case notificationHold:
				<-ctx.Done()
				return // KeepQueued blocks this ordered queue without ACK.
			case notificationACK, notificationDrop:
				if d.Ack(false) != nil {
					return
				}
				s.metric(target, func(m *notificationMetrics) {
					if action == notificationDrop {
						m.dropped++
					} else {
						m.pushed++
					}
					m.stuck = false
				})
			}
			break
		}
	}
}

func (s *Service) openRabbit(ctx context.Context, name string) (*amqp.Connection, error) {
	user, ok := os.LookupEnv("RABBITMQ_USERNAME")
	password, present := os.LookupEnv("RABBITMQ_PASSWORD")
	if !ok || !present {
		return nil, errors.New("RabbitMQ credentials must be supplied in environment")
	}
	c := s.Config.Rabbit
	scheme := "amqp"
	stopHandshake := func() bool { return false }
	defer func() { stopHandshake() }()
	config := amqp.Config{Properties: amqp.Table{"connection_name": name}, Dial: func(network, address string) (net.Conn, error) {
		conn, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, address)
		if err != nil {
			return nil, err
		}
		if err = conn.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
			_ = conn.Close()
			return nil, err
		}
		// Custom Dial must retain the library's bounded TLS/AMQP handshake.
		// openComplete clears this deadline after negotiation.
		stopHandshake = context.AfterFunc(ctx, func() { _ = conn.Close() })
		return conn, nil
	}}
	if c.TLS {
		scheme = "amqps"
		var err error
		config.TLSClientConfig, err = verifiedTLS(c.Host, c.CA)
		if err != nil {
			return nil, err
		}
	}
	uri := url.URL{Scheme: scheme, Host: c.Address(), User: url.UserPassword(user, password), Path: "/" + c.VHost, RawPath: "/" + url.PathEscape(c.VHost)}
	conn, err := amqp.DialConfig(uri.String(), config)
	if err != nil {
		return nil, errors.New("RabbitMQ initialization failed")
	}
	return conn, nil
}

func (s *Service) remoteDomains(ctx context.Context, client *http.Client) ([]string, error) {
	c := s.Config.Rabbit
	scheme := "http"
	if c.TLS {
		scheme = "https"
	}
	user := os.Getenv("RABBITMQ_USERNAME")
	password := os.Getenv("RABBITMQ_PASSWORD")
	domains := []string{}
	for page := 1; ; page++ {
		base := scheme + "://" + (Endpoint{c.AdminHost, c.AdminPort}).Address() + "/api/queues/" + url.PathEscape(c.VHost)
		query := url.Values{"name": {"^backend-notifications\\..*"}, "use_regex": {"true"}, "page_size": {"100"}, "page": {strconv.Itoa(page)}}
		req, err := http.NewRequestWithContext(ctx, "GET", base+"?"+query.Encode(), nil)
		if err != nil {
			return nil, errors.New("invalid queue discovery endpoint")
		}
		req.SetBasicAuth(user, password)
		resp, err := client.Do(req)
		if err != nil {
			return nil, errors.New("queue discovery failed")
		}
		var data struct {
			Items []struct {
				Name string `json:"name"`
			} `json:"items"`
			Page  int `json:"page"`
			Pages int `json:"page_count"`
		}
		err = json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&data)
		_ = resp.Body.Close()
		if err != nil || resp.StatusCode != 200 || data.Page != page || data.Pages < 0 {
			return nil, errors.New("invalid queue discovery response")
		}
		for _, q := range data.Items {
			suffix, ok := strings.CutPrefix(q.Name, "backend-notifications.")
			if ok && validDomain(suffix) {
				domains = append(domains, suffix)
			}
		}
		if page >= data.Pages {
			return domains, nil
		}
	}
}

func (s *Service) runPusher(ctx context.Context) {
	if s.Config.Rabbit.AdminHost == "" {
		return
	} // Source AMQP-only mode leaves pusher readiness false.
	transport := &http.Transport{Proxy: nil, ResponseHeaderTimeout: 10 * time.Second}
	if s.Config.Rabbit.TLS {
		config, err := verifiedTLS(s.Config.Rabbit.AdminHost, s.Config.Rabbit.CA)
		if err != nil {
			return
		}
		transport.TLSClientConfig = config
	}
	defer transport.CloseIdleConnections()
	admin := &http.Client{Transport: transport, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	federator := notificationClient()
	defer federator.CloseIdleConnections()
	for ctx.Err() == nil {
		conn, err := s.openRabbit(ctx, "backend-notification-pusher")
		if err == nil {
			channel, e := conn.Channel()
			err = e
			if err == nil {
				err = s.pushSession(ctx, channel, admin, federator)
				_ = channel.Close()
			}
			_ = conn.Close()
		}
		s.mark(0, false)
		if !wait(ctx, time.Second) {
			return
		}
	}
}

func (s *Service) pushSession(ctx context.Context, channel *amqp.Channel, admin, federator *http.Client) error {
	if err := channel.Qos(1, 0, false); err != nil {
		return err
	}
	s.mark(0, true)
	workerCtx, stop := context.WithCancel(ctx)
	defer stop()
	type consumer struct {
		tag  string
		stop context.CancelFunc
	}
	consumers := map[string]consumer{}
	var wg sync.WaitGroup
	nextConsumer := 0
	defer func() {
		stop()
		for _, c := range consumers {
			_ = channel.Cancel(c.tag, false)
		}
		wg.Wait()
	}()
	closed := channel.NotifyClose(make(chan *amqp.Error, 1))
	for ctx.Err() == nil {
		domains, err := s.remoteDomains(ctx, admin)
		if err != nil {
			return err
		}
		current := make(map[string]bool)
		for _, domain := range domains {
			current[domain] = true
			if _, exists := consumers[domain]; exists {
				continue
			}
			name := "backend-notifications." + domain
			if _, err = channel.QueueDeclare(name, true, false, false, false, amqp.Table{"x-queue-type": "quorum", "x-single-active-consumer": true}); err != nil {
				return err
			}
			nextConsumer++
			tag := "pusher." + strconv.Itoa(nextConsumer)
			deliveries, e := channel.Consume(name, tag, false, false, false, false, nil)
			if e != nil {
				return e
			}
			consumerCtx, cancel := context.WithCancel(workerCtx)
			consumers[domain] = consumer{tag: tag, stop: cancel}
			wg.Go(func() { s.consumeNotifications(consumerCtx, federator, domain, deliveries) })
		}
		for domain, c := range consumers {
			if !current[domain] {
				c.stop()
				if err = channel.Cancel(c.tag, false); err != nil {
					return err
				}
				delete(consumers, domain)
			}
		}
		timer := time.NewTimer(time.Duration(s.Config.Pusher.Refresh) * time.Microsecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-closed:
			timer.Stop()
			return errors.New("notification channel closed")
		case <-timer.C:
		}
	}
	return nil
}
