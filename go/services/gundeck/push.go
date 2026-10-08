package gundeck

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
)

type Recipient struct {
	User    string   `json:"user_id"`
	Route   string   `json:"route"`
	Clients []string `json:"clients"`
}
type Push struct {
	Recipients       []Recipient     `json:"recipients"`
	Origin           *string         `json:"origin"`
	OriginConnection *string         `json:"origin_connection"`
	Transient        bool            `json:"transient"`
	IncludeOrigin    *bool           `json:"native_include_origin"`
	Encrypt          *bool           `json:"native_encrypt"`
	APS              json.RawMessage `json:"native_aps"`
	Priority         string          `json:"native_priority"`
	Payload          json.RawMessage `json:"payload"`
	Cells            bool            `json:"is_cells_event"`
}
type liveNotification struct {
	Notification
	Transient bool `json:"transient"`
}
type newNotification struct {
	push         Push
	notification liveNotification
}
type wireClient struct {
	ID           string   `json:"id"`
	Capabilities []string `json:"capabilities"`
}

func (c wireClient) consumable() bool {
	for _, v := range c.Capabilities {
		if v == "consumable-notifications" {
			return true
		}
	}
	return false
}
func validatePushes(raw []byte) ([]Push, error) {
	var pushes []Push
	if err := json.Unmarshal(raw, &pushes); err != nil || pushes == nil {
		return nil, fmt.Errorf("push array required")
	}
	for i := range pushes {
		p := &pushes[i]
		if p.Recipients == nil || !validPayload(p.Payload) || (p.Origin != nil && !uuidValid(*p.Origin)) || (p.Priority != "" && p.Priority != "high" && p.Priority != "low") {
			return nil, fmt.Errorf("invalid push")
		}
		if p.Priority == "" {
			p.Priority = "high"
		}
		if p.Origin != nil {
			value := strings.ToLower(*p.Origin)
			p.Origin = &value
		}
		unique := map[string]bool{}
		recipients := []Recipient{}
		for _, r := range p.Recipients {
			if !uuidValid(r.User) || (r.Route != "any" && r.Route != "direct") || r.Clients == nil {
				return nil, fmt.Errorf("invalid recipient")
			}
			r.User = strings.ToLower(r.User)
			for j, client := range r.Clients {
				if !clientValid(client) {
					return nil, fmt.Errorf("invalid recipient client")
				}
				r.Clients[j] = strings.ToLower(client)
			}
			encoded, _ := json.Marshal(r)
			if !unique[string(encoded)] {
				recipients = append(recipients, r)
				unique[string(encoded)] = true
			}
		}
		sort.Slice(recipients, func(i, j int) bool { return recipients[i].User < recipients[j].User })
		p.Recipients = recipients
	}
	return pushes, nil
}
func (s *Server) getClients(ctx context.Context, pushes []Push) (map[string][]wireClient, error) {
	seen := map[string]bool{}
	for _, push := range pushes {
		for _, recipient := range push.Recipients {
			seen[recipient.User] = true
		}
	}
	users := []string{}
	for user := range seen {
		users = append(users, user)
	}
	sort.Strings(users)
	result := map[string][]wireClient{}
	for first := 0; first < len(users); first += 100 {
		end := min(first+100, len(users))
		body, _ := json.Marshal(map[string]any{"users": users[first:end]})
		response, err := s.rpcJSON(ctx, "http://"+s.Config.Brig.Address()+"/i/clients/full", body, 10*time.Second)
		if err != nil {
			return nil, err
		}
		var batch map[string][]wireClient
		if err = json.Unmarshal(response, &batch); err != nil {
			return nil, fmt.Errorf("invalid clients response")
		}
		for user, clients := range batch {
			result[user] = clients
		}
	}
	return result, nil
}
func selectedClient(clients []string, client string) bool {
	if len(clients) == 0 {
		return true
	}
	for _, id := range clients {
		if id == client {
			return true
		}
	}
	return false
}
func splitRecipients(push Push, clients map[string][]wireClient) ([]Recipient, []Recipient) {
	legacy := []Recipient{}
	queue := []Recipient{}
	for _, recipient := range push.Recipients {
		old := []string{}
		modern := []string{}
		for _, client := range clients[recipient.User] {
			if !selectedClient(recipient.Clients, client.ID) {
				continue
			}
			if client.consumable() {
				modern = append(modern, client.ID)
			} else {
				old = append(old, client.ID)
			}
		}
		switch {
		case len(modern) == 0:
			legacy = append(legacy, recipient)
		case len(old) == 0:
			queue = append(queue, recipient)
		default:
			a := recipient
			a.Clients = old
			legacy = append(legacy, a)
			b := recipient
			if len(recipient.Clients) > 0 {
				b.Clients = modern
			}
			queue = append(queue, b)
		}
	}
	return legacy, queue
}
func newNotice(push Push, recipients []Recipient) newNotification {
	push.Recipients = recipients
	return newNotification{push: push, notification: liveNotification{Notification: Notification{ID: gocql.TimeUUID().String(), Payload: push.Payload}, Transient: push.Transient}}
}
func (s *Server) storeNotification(ctx context.Context, notification Notification, recipients []Recipient) error {
	id, _ := gocql.ParseUUID(notification.ID)
	ttl := int32(s.Config.Settings.TTL)
	if len(recipients) == 0 {
		return nil
	}
	var reference gocql.UUID
	if len(recipients) > 1 {
		var err error
		reference, err = gocql.RandomUUID()
		if err != nil {
			return err
		}
		if err = s.query(ctx, "INSERT INTO notification_payload (id, payload) VALUES (?, ?) USING TTL ?", reference, []byte(notification.Payload), ttl).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5}).Exec(); err != nil {
			return err
		}
	}
	return parallel(len(recipients), 32, func(i int) error {
		recipient := recipients[i]
		user, _ := gocql.ParseUUID(recipient.User)
		if len(recipients) == 1 {
			return s.query(ctx, "INSERT INTO notifications (user, id, payload, clients) VALUES (?, ?, ?, ?) USING TTL ?", user, id, []byte(notification.Payload), recipient.Clients, ttl).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5}).Exec()
		}
		return s.query(ctx, "INSERT INTO notifications (user, id, payload_ref, payload_ref_size, clients) VALUES (?, ?, ?, ?, ?) USING TTL ?", user, id, reference, int32(len(notification.Payload)), recipient.Clients, ttl).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5}).Exec()
	})
}
func parallel(count, limit int, work func(int) error) error {
	var wg sync.WaitGroup
	var mu sync.Mutex
	var failure error
	jobs := make(chan int)
	for i := 0; i < min(count, max(limit, 1)); i++ {
		wg.Go(func() {
			for i := range jobs {
				if err := work(i); err != nil {
					mu.Lock()
					if failure == nil {
						failure = err
					}
					mu.Unlock()
				}
			}
		})
	}
	for i := 0; i < count; i++ {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	return failure
}
func (s *Server) pushAll(ctx context.Context, pushes []Push) error {
	clients, err := s.getClients(ctx, pushes)
	if err != nil {
		return err
	}
	legacy := []newNotification{}
	modern := []newNotification{}
	for _, push := range pushes {
		old, queue := push.Recipients, []Recipient{}
		if s.Config.Settings.Consumable {
			old, queue = splitRecipients(push, clients)
		}
		if len(old) > 0 {
			notice := newNotice(push, old)
			legacy = append(legacy, notice)
			if !push.Transient {
				if err = s.storeNotification(ctx, notice.notification.Notification, old); err != nil {
					return err
				}
			}
		}
		if len(queue) > 0 {
			modern = append(modern, newNotice(push, queue))
		}
	}
	if len(legacy) > 0 {
		if err = s.forkDelivery(ctx, func(ctx context.Context) { s.deliverLegacy(ctx, legacy, clients) }); err != nil {
			return err
		}
	}
	for _, notice := range modern {
		payload, _ := json.Marshal(notice.notification.Notification)
		keys := map[string]bool{}
		for _, r := range notice.push.Recipients {
			if len(r.Clients) == 0 {
				keys[r.User] = true
			} else {
				for _, client := range r.Clients {
					keys[r.User+"."+client] = true
				}
			}
		}
		for key := range keys {
			if err = s.publishQueue(ctx, notificationExchange, key, payload, notice.push.Transient); err != nil {
				return err
			}
		}
	}
	if len(modern) > 0 {
		if err = s.forkDelivery(ctx, func(ctx context.Context) {
			for _, notice := range modern {
				excluded := map[string]bool{}
				for user, list := range clients {
					for _, client := range list {
						if !client.consumable() {
							excluded[user+":"+client.ID] = true
						}
					}
				}
				s.deliverNative(ctx, notice, excluded)
			}
		}); err != nil {
			return err
		}
	}
	if s.Config.Settings.CellsQueue != nil {
		for _, push := range pushes {
			if !push.Cells {
				continue
			}
			notice := newNotice(push, push.Recipients)
			payload, _ := json.Marshal(notice.notification.Notification)
			if err = s.publishQueue(ctx, "", *s.Config.Settings.CellsQueue, payload, push.Transient); err != nil {
				return err
			}
		}
	}
	return nil
}
func (s *Server) rpcJSON(ctx context.Context, uri string, body []byte, timeout time.Duration) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, uri, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	if id, ok := ctx.Value(requestIDKey{}).(string); ok && id != "" {
		request.Header.Set("Request-Id", id)
	}
	response, err := s.http.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, response.Body)
		return nil, fmt.Errorf("peer request failed")
	}
	return io.ReadAll(response.Body)
}
func (s *Server) forkDelivery(ctx context.Context, work func(context.Context)) error {
	select {
	case s.deliverySlots <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	s.deliveryWG.Go(func() {
		defer func() { <-s.deliverySlots }()
		deliveryCtx := s.background
		if id := ctx.Value(requestIDKey{}); id != nil {
			deliveryCtx = context.WithValue(deliveryCtx, requestIDKey{}, id)
		}
		work(deliveryCtx)
	})
	return nil
}

type pushTarget struct {
	User       string `json:"user_id"`
	Connection string `json:"conn_id"`
}
type bulkRequestItem struct {
	Notification liveNotification `json:"notification"`
	Targets      []pushTarget     `json:"targets"`
}
type bulkResponseItem struct {
	ID     string     `json:"notif_id"`
	Target pushTarget `json:"target"`
	Status string     `json:"status"`
}

func (s *Server) deliverLegacy(ctx context.Context, notices []newNotification, clients map[string][]wireClient) {
	requests := map[string]map[string]*bulkRequestItem{}
	presences := map[string]Presence{}
	sent := map[string]map[string]bool{}
	for _, notice := range notices {
		for _, recipient := range notice.push.Recipients {
			list, err := s.listPresence(ctx, recipient.User)
			if err != nil {
				s.logger.Event("Error", "Presence lookup failed")
				continue
			}
			for _, presence := range list {
				if notice.push.Origin != nil && notice.push.OriginConnection != nil && presence.User == *notice.push.Origin && presence.Device == *notice.push.OriginConnection {
					continue
				}
				if presence.Client != nil && !selectedClient(recipient.Clients, *presence.Client) {
					continue
				}
				resource, err := url.Parse(presence.Resource)
				if err != nil {
					continue
				}
				resource.Path = "/i/bulkpush"
				uri := resource.String()
				if requests[uri] == nil {
					requests[uri] = map[string]*bulkRequestItem{}
				}
				if requests[uri][notice.notification.ID] == nil {
					requests[uri][notice.notification.ID] = &bulkRequestItem{Notification: notice.notification, Targets: []pushTarget{}}
				}
				requests[uri][notice.notification.ID].Targets = append(requests[uri][notice.notification.ID].Targets, pushTarget{presence.User, presence.Device})
				presences[uri+":"+presence.User+":"+presence.Device] = presence
			}
		}
	}
	uris := []string{}
	for uri := range requests {
		uris = append(uris, uri)
	}
	var mu sync.Mutex
	_ = parallel(len(uris), 32, func(i int) error {
		uri := uris[i]
		items := []bulkRequestItem{}
		for _, item := range requests[uri] {
			items = append(items, *item)
		}
		body, _ := json.Marshal(map[string]any{"bulkpush_req": items})
		var raw []byte
		var err error
		for attempt := 0; attempt < 2; attempt++ {
			raw, err = s.rpcJSON(ctx, uri, body, 3*time.Second)
			if err == nil {
				break
			}
		}
		var response struct {
			Items []bulkResponseItem `json:"bulkpush_resp"`
		}
		if err == nil {
			err = json.Unmarshal(raw, &response)
		}
		if err != nil {
			for key, presence := range presences {
				if strings.HasPrefix(key, uri+":") && time.Now().UnixMilli()-presence.Created > 10*24*60*60*1000 {
					_ = s.removePresence(ctx, presence)
				}
			}
			s.logger.Event("Warn", "Cannon bulk push failed")
			return nil
		}
		for _, item := range response.Items {
			presence, ok := presences[uri+":"+item.Target.User+":"+item.Target.Connection]
			if !ok || requests[uri][item.ID] == nil {
				s.logger.Event("Error", "Invalid cannon bulk response")
				continue
			}
			if item.Status == "push_status_gone" {
				_ = s.removePresence(ctx, presence)
			} else if item.Status == "push_status_ok" && presence.Client != nil {
				mu.Lock()
				if sent[item.ID] == nil {
					sent[item.ID] = map[string]bool{}
				}
				sent[item.ID][presence.User+":"+*presence.Client] = true
				mu.Unlock()
			}
		}
		return nil
	})
	for _, notice := range notices {
		excluded := sent[notice.notification.ID]
		if excluded == nil {
			excluded = map[string]bool{}
		}
		for user, list := range clients {
			for _, client := range list {
				if client.consumable() {
					excluded[user+":"+client.ID] = true
				}
			}
		}
		s.deliverNative(ctx, notice, excluded)
	}
}
func nativeEnvelope(id, user, transport, priority string) (string, error) {
	notice := map[string]any{"type": "notice", "data": map[string]string{"id": id}, "user": user}
	var message any
	if transport == "GCM" {
		if priority == "low" {
			priority = "normal"
		}
		message = map[string]any{"data": notice, "priority": priority}
	} else {
		aps := map[string]any{"alert": map[string]string{"title": "New message"}, "mutable-content": "1"}
		if strings.HasPrefix(transport, "APNS_VOIP") {
			aps = map[string]any{}
		}
		message = map[string]any{"data": notice, "aps": aps}
	}
	inner, err := json.Marshal(message)
	if err != nil {
		return "", err
	}
	maximum := 4096
	if strings.HasPrefix(transport, "APNS_VOIP") {
		maximum = 5120
	}
	if len(inner) > maximum {
		return "", fmt.Errorf("native payload too large")
	}
	outer, err := json.Marshal(map[string]string{transport: string(inner)})
	return string(outer), err
}
func (s *Server) deliverNative(ctx context.Context, notice newNotification, excluded map[string]bool) {
	if notice.push.Transient {
		return
	}
	concurrency := max(len(notice.push.Recipients), 1)
	if s.Config.Settings.NativeConcurrency != nil {
		concurrency = *s.Config.Settings.NativeConcurrency
	}
	cost := 0
	for _, recipient := range notice.push.Recipients {
		if recipient.Route != "direct" {
			cost++
		}
	}
	cost = min(cost, concurrency)
	if !s.acquireBudget(cost) {
		return
	}
	defer s.releaseBudget(cost)
	_ = parallel(len(notice.push.Recipients), concurrency, func(i int) error {
		recipient := notice.push.Recipients[i]
		if recipient.Route == "direct" || (notice.push.IncludeOrigin != nil && !*notice.push.IncludeOrigin && notice.push.Origin != nil && recipient.User == *notice.push.Origin) {
			return nil
		}
		addresses, err := s.lookupTokens(ctx, recipient.User, false)
		if err != nil {
			s.logger.Event("Error", "Native token lookup failed")
			return nil
		}
		for _, address := range addresses {
			if !selectedClient(recipient.Clients, address.Client) || excluded[address.User+":"+address.Client] {
				continue
			}
			if notice.push.Origin != nil && notice.push.OriginConnection != nil && address.User == *notice.push.Origin && string(address.Connection) == *notice.push.OriginConnection {
				continue
			}
			s.publishNative(ctx, notice.notification.ID, notice.push.Priority, address)
		}
		return nil
	})
}
func (s *Server) publishNative(ctx context.Context, id, priority string, address pushAddress) {
	s.publishNativeAttempt(ctx, id, priority, address, 0)
}
func (s *Server) publishNativeAttempt(ctx context.Context, id, priority string, address pushAddress, attempt int) {
	message, err := nativeEnvelope(id, address.User, address.Transport, priority)
	if err != nil {
		s.count("push_native_too_large")
		s.logger.Event("Warn", "Native payload rejected")
		return
	}
	_, err = s.sns.Publish(ctx, &sns.PublishInput{TargetArn: aws.String(address.ARN), Message: aws.String(message), MessageStructure: aws.String("json")}, func(o *sns.Options) { o.RetryMaxAttempts = 4 })
	if err == nil {
		s.count("push_native_success")
		return
	}
	code, messageText := serviceError(err)
	switch code {
	case "EndpointDisabled":
		s.count("push_native_disabled")
		s.tokenMu.Lock()
		err = s.deleteTokens(ctx, []pushAddress{address}, nil)
		s.tokenMu.Unlock()
		if err == nil {
			err = s.tokenRemoved(ctx, address, false)
		}
	case "AuthorizationError", "AuthorizationErrorException":
		if attempt < 1 {
			arn, fresh, createErr := s.createEndpoint(ctx, address)
			if createErr == nil && fresh {
				user, _ := gocql.ParseUUID(address.User)
				createErr = s.query(ctx, "UPDATE user_push SET arn = ? WHERE usr = ? AND transport = ? AND app = ? AND ptoken = ?", arn, user, transportCode(address.Transport), address.App, address.Token).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5}).Exec()
				if createErr == nil {
					address.ARN = arn
					s.publishNativeAttempt(ctx, id, priority, address, attempt+1)
					return
				}
			}
			if createErr != nil {
				s.logger.Event("Warn", "SNS endpoint recreation failed")
			}
			return
		}
		s.count("push_native_unauthorized")
	case "InvalidParameter":
		if strings.HasPrefix(messageText, "Invalid parameter: Message too long") || strings.HasPrefix(messageText, "Invalid parameter: Message Reason: Invalid notification for protocol ") {
			s.count("push_native_too_large")
			return
		}
		if !strings.HasPrefix(messageText, "Invalid parameter: TargetArn Reason: No endpoint found for the target arn specified") {
			s.count("push_native_errors")
			return
		}
		fallthrough
	case "NotFound", "NotFoundException":
		s.count("push_native_invalid")
		err = s.deleteTokenRow(ctx, address)
		if err == nil {
			err = s.tokenRemoved(ctx, address, false)
		}
	default:
		s.count("push_native_errors")
	}
	if err != nil {
		s.logger.Event("Warn", "Native push failed")
	}
}
func (s *Server) tokenRemoved(ctx context.Context, address pushAddress, websocket bool) error {
	payload, _ := json.Marshal([]any{map[string]any{"type": "user.push-remove", "token": address.PushToken}})
	push := Push{Recipients: []Recipient{{User: address.User, Route: "direct", Clients: []string{address.Client}}}, Payload: payload, Priority: "high"}
	notice := newNotice(push, push.Recipients)
	if websocket {
		s.deliverLegacy(ctx, []newNotification{notice}, nil)
	}
	return s.storeNotification(ctx, notice.notification.Notification, push.Recipients)
}

func (s *Server) acquireBudget(cost int) bool {
	s.budgetMu.Lock()
	defer s.budgetMu.Unlock()
	if budget := s.Config.Settings.NativeBudget; budget != nil {
		if budget.Hard != nil && s.budgetSpent >= *budget.Hard {
			s.count("net_nativepush_thread_budget_hard_limit_breached")
			return false
		}
		if budget.Soft != nil && s.budgetSpent >= *budget.Soft {
			s.count("net_nativepush_thread_budget_soft_limit_breached")
		}
	}
	s.budgetSpent += cost
	return true
}
func (s *Server) releaseBudget(cost int) {
	s.budgetMu.Lock()
	s.budgetSpent -= cost
	s.budgetMu.Unlock()
}
