// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"golang.org/x/sync/errgroup"
)

type notificationEnqueuer interface {
	Enqueue(context.Context, string, payloadBundle) error
}

type botEventDelivery interface {
	DeliverAsync(context.Context, []localConversationMember, map[string]any)
}

type conversationJoinNotifier struct {
	Queue   notificationEnqueuer
	Bots    botEventDelivery
	Client  *http.Client
	Gundeck Endpoint
	Domain  string
	Now     func() time.Time
}

func joinEvent(join groupJoin, now time.Time, domain string) map[string]any {
	members := make([]map[string]any, 0, len(join.NewUsers))
	ids := make([]string, 0, len(join.NewUsers))
	for _, user := range join.NewUsers {
		members = append(members, map[string]any{"qualified_id": user, "id": user.ID, "conversation_role": "wire_member"})
		ids = append(ids, user.ID)
	}
	event := map[string]any{"type": "conversation.member-join", "data": map[string]any{"users": members, "user_ids": ids, "add_type": "external_add"}, "conversation": join.Conversation.ID, "qualified_conversation": qualifiedUser{ID: join.Conversation.ID, Domain: domain}, "from": join.Actor.ID, "qualified_from": join.Actor, "via": join.Via, "time": now.UTC().Truncate(time.Millisecond).Format("2006-01-02T15:04:05.000Z")}
	if join.Conversation.Team != nil {
		event["team"] = *join.Conversation.Team
	}
	return event
}

func joinBundle(join groupJoin, now time.Time, domain string, recipients []string) (payloadBundle, error) {
	action := map[string]any{"tag": "ConversationJoinTag", "action": map[string]any{"users": join.NewUsers, "role": "wire_member", "join_type": "external_add"}}
	modern := map[string]any{"time": now.UTC(), "origUserId": join.Actor, "convId": join.Conversation.ID, "alreadyPresentUsers": recipients, "action": action, "extraConversationData": map[string]any{"group_id": nil}}
	legacy := map[string]any{"cuTime": now.UTC(), "cuOrigUserId": join.Actor, "cuConvId": join.Conversation.ID, "cuAlreadyPresentUsers": recipients, "cuAction": action}
	bundle := payloadBundle{Policy: stringPtr("keep_queued")}
	for i, body := range []any{modern, legacy} {
		encoded, err := json.Marshal(body)
		if err != nil {
			return payloadBundle{}, errors.New("conversation notification serialization failed")
		}
		versions := &versionRange{From: intPtr(1)}
		if i == 1 {
			versions = &versionRange{From: intPtr(0), Until: intPtr(1)}
		}
		bundle.Notifications = append(bundle.Notifications, notification{Origin: stringPtr(domain), Component: stringPtr("galley"), Path: stringPtr("on-conversation-updated"), Body: stringPtr(string(encoded)), Versions: versions, RequestID: stringPtr(join.RequestID)})
	}
	return bundle, nil
}

func stringPtr(value string) *string { return &value }
func intPtr(value int) *int          { return &value }

func (n *conversationJoinNotifier) NotifyJoin(ctx context.Context, join groupJoin) error {
	if n.Client == nil || !n.Gundeck.valid() || !validDomain(n.Domain) || join.Conversation == nil || len(join.NewUsers) == 0 {
		return errors.New("conversation notifier is unavailable")
	}
	if len(join.Bots) > 0 && n.Bots == nil || len(join.Remotes) > 0 && n.Queue == nil {
		return errors.New("conversation recipient delivery is unavailable")
	}
	now := time.Now()
	if n.Now != nil {
		now = n.Now()
	}
	domain := strings.ToLower(n.Domain)
	buckets := make(map[string][]string)
	for _, user := range join.Remotes {
		target := strings.ToLower(user.Domain)
		if target != strings.ToLower(join.Actor.Domain) {
			buckets[target] = append(buckets[target], user.ID)
		}
	}
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(8) // Source pooledForConcurrentlyN8, before local delivery.
	for target, users := range buckets {
		slices.Sort(users)
		bundle, err := joinBundle(join, now, domain, slices.Compact(users))
		if err != nil {
			return err
		}
		group.Go(func() error {
			for attempt := 0; ; attempt++ {
				if err := n.Queue.Enqueue(groupCtx, target, bundle); err == nil {
					return nil
				}
				if attempt == 3 {
					return errors.New("conversation federation enqueue failed")
				}
				if !wait(groupCtx, time.Second) {
					return groupCtx.Err()
				}
			}
		})
	}
	if err := group.Wait(); err != nil {
		return err
	}
	event := joinEvent(join, now, domain)
	if err := n.pushLocal(ctx, join, event); err != nil {
		return err
	}
	if len(join.Bots) > 0 {
		n.Bots.DeliverAsync(ctx, join.Bots, event)
	}
	return nil
}

func (n *conversationJoinNotifier) pushLocal(ctx context.Context, join groupJoin, event map[string]any) error {
	// Effects passes defaultNotificationSubsystemConfig: its fanout limit is
	// the source hard limit2000 and its chunk size128, independent of maxTeamSize.
	if len(join.Locals) > 2000 {
		return nil
	}
	users := slices.Clone(join.Locals)
	slices.Sort(users)
	users = slices.Compact(users)
	group, groupCtx := errgroup.WithContext(ctx)
	// An empty recipient set still emits a push, including Cells routing. The
	// source chunker retains an empty push instead of dropping it.
	for start := 0; start < len(users) || start == 0; start += 128 {
		end := min(start+128, len(users))
		recipients := make([]map[string]any, 0, end-start)
		for _, id := range users[start:end] {
			recipients = append(recipients, map[string]any{"user_id": id, "route": "any", "clients": []string{}})
		}
		cells, _ := join.Conversation.Metadata["cells_state"].(int32)
		push := map[string]any{"recipients": recipients, "payload": []any{event}, "is_cells_event": cells != 0}
		if strings.EqualFold(join.Actor.Domain, n.Domain) {
			push["origin"] = join.Actor.ID
		}
		body, err := json.Marshal([]any{push})
		if err != nil {
			return errors.New("conversation push serialization failed")
		}
		group.Go(func() error {
			// Preserve source chunked POST/no transport-level replay. Overall job
			// retries remain owned by the job consumer, not this HTTP operation.
			req, err := http.NewRequestWithContext(groupCtx, "POST", "http://"+n.Gundeck.Address()+"/i/push/v2", io.NopCloser(bytes.NewReader(body)))
			if err != nil {
				return errors.New("invalid conversation push request")
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Request-Id", join.RequestID)
			req.ContentLength = -1
			resp, err := n.Client.Do(req)
			if err != nil {
				return errors.New("conversation push transport failed")
			}
			defer resp.Body.Close()
			if _, err = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20)); err != nil {
				return errors.New("conversation push response failed")
			}
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				return errors.New("conversation push failed")
			}
			return nil
		})
	}
	return group.Wait()
}

type rabbitJobPublisher struct{ Channel *amqp.Channel }

func (p rabbitJobPublisher) Publish(ctx context.Context, job backgroundJob) error {
	return publishBackgroundJob(ctx, p.Channel, job)
}

func (p rabbitJobPublisher) Enqueue(ctx context.Context, domain string, bundle payloadBundle) error {
	if !validDomain(domain) {
		return errors.New("invalid notification target domain")
	}
	name := "backend-notifications." + strings.ToLower(domain)
	if _, err := p.Channel.QueueDeclare(name, true, false, false, false, amqp.Table{"x-queue-type": "quorum", "x-single-active-consumer": true}); err != nil {
		return errors.New("notification queue declaration failed")
	}
	body, err := json.Marshal(bundle)
	if err != nil {
		return errors.New("notification bundle serialization failed")
	}
	if err = p.Channel.PublishWithContext(ctx, "", name, false, false, amqp.Publishing{ContentType: "application/json", DeliveryMode: amqp.Persistent, Body: body}); err != nil {
		return errors.New("notification bundle publication failed")
	}
	return nil
}
