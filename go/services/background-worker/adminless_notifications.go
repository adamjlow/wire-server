// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"
)

// Reuse the established Gundeck transport, Rabbit enqueuer and asynchronous bot
// boundary. Adminless origin/Cells/version contracts differ from group joins.
type adminlessNotifier struct {
	Base             conversationJoinNotifier
	Federation       *Service
	FederationClient *http.Client
}

func (n *adminlessNotifier) SupportsSystemDelete(ctx context.Context, target, requestID string) (bool, error) {
	if n.Federation == nil || !n.Federation.Config.Federator.valid() || n.FederationClient == nil || !validDomain(target) || !validDomain(n.Base.Domain) {
		return false, errors.New("adminless federation unavailable")
	}
	descriptor := notification{Origin: stringPtr(n.Base.Domain), RequestID: stringPtr(requestID)}
	// runFederatorClient negotiates before executing the explicit api-version RPC:
	// two requests, unit JSON array, unversioned header0 then highest common version.
	body, err := n.Federation.federationRequest(ctx, n.FederationClient, target, descriptor, "api-version", "brig", nil, []byte("[]"))
	if err != nil {
		return false, err
	}
	versions, err := adminlessVersionInfo(body)
	if err != nil {
		return false, err
	}
	best := -1
	for _, v := range versions {
		if v >= 0 && v <= 4 && int(v) > best {
			best = int(v)
		}
	}
	if best < 0 {
		return false, errors.New("no common federation version")
	}
	body, err = n.Federation.federationRequest(ctx, n.FederationClient, target, descriptor, "api-version", "brig", &best, []byte("[]"))
	if err != nil {
		return false, err
	}
	versions, err = adminlessVersionInfo(body)
	if err != nil {
		return false, err
	}
	for _, v := range versions {
		if v == 4 {
			return true, nil
		}
	}
	return false, nil
}

func (n *adminlessNotifier) Promoted(ctx context.Context, conv *storedConversation, target qualifiedUser, actor *string, requestID string) error {
	data := map[string]any{"target": target.ID, "qualified_target": target, "conversation_role": "wire_admin"}
	action := map[string]any{"tag": "ConversationMemberUpdateTag", "action": map[string]any{"target": target, "update": map[string]any{"conversation_role": "wire_admin"}}}
	return n.deliver(ctx, conv, actor, requestID, "member-update", data, action)
}
func (n *adminlessNotifier) Deleted(ctx context.Context, conv *storedConversation, actor *string, requestID string) error {
	action := map[string]any{"tag": "ConversationDeleteTag", "action": map[string]any{}}
	return n.deliver(ctx, conv, actor, requestID, "delete", nil, action)
}
func (n *adminlessNotifier) Reminder(ctx context.Context, conv *storedConversation, actor *string, requestID, timestamp string) error {
	canonical, ok := canonicalAdminlessNotificationMillis(timestamp)
	if !ok {
		return errors.New("invalid adminless deletion timestamp")
	}
	data := map[string]any{"deletion_scheduled_for": canonical}
	return n.deliver(ctx, conv, actor, requestID, "adminless-reminder", data, nil)
}
func (n *adminlessNotifier) deliver(ctx context.Context, conv *storedConversation, actor *string, requestID, kind string, data, action any) error {
	if conv == nil || n.Base.Client == nil || !n.Base.Gundeck.valid() || !validDomain(n.Base.Domain) {
		return errors.New("adminless notifier unavailable")
	}
	domain := strings.ToLower(n.Base.Domain)
	now := time.Now()
	if n.Base.Now != nil {
		now = n.Base.Now()
	}
	now = now.UTC()
	buckets := map[string][]string{}
	for _, member := range conv.RemoteMembers {
		target := strings.ToLower(member.User.Domain)
		if actor != nil && kind != "adminless-reminder" && target == domain {
			continue
		}
		buckets[target] = append(buckets[target], member.User.ID)
	}
	if len(buckets) > 0 && n.Base.Queue == nil {
		return errors.New("adminless federation queue unavailable")
	}
	system := actor == nil
	local := []string{}
	bots := []localConversationMember{}
	for _, member := range conv.LocalMembers {
		if !system && kind != "adminless-reminder" && member.bot() {
			bots = append(bots, member)
		} else {
			local = append(local, member.ID)
		}
	}
	if len(bots) > 0 && n.Base.Bots == nil {
		return errors.New("adminless bot delivery unavailable")
	}
	slices.Sort(local)
	local = slices.Compact(local)
	event := map[string]any{"conversation": conv.ID, "qualified_conversation": qualifiedUser{ID: conv.ID, Domain: domain}, "time": now.Truncate(time.Millisecond).Format("2006-01-02T15:04:05.000Z"), "data": data}
	if conv.Team != nil {
		event["team"] = *conv.Team
	}
	eventType := "conversation." + kind
	actorID := qualifiedUser{Domain: domain}
	if actor != nil {
		actorID.ID = *actor
		event["from"] = *actor
		event["qualified_from"] = actorID
		event["via"] = "user"
	} else {
		eventType = "conversation.system." + kind
		event["via"] = "system"
	}
	if !system && kind == "delete" {
		if groupType, ok := conv.Metadata["group_conv_type"].(int32); ok && groupType == 2 {
			eventType = "conversation.delete-meeting"
		}
	}
	event["type"] = eventType
	remote, remoteCtx := errgroup.WithContext(ctx)
	remote.SetLimit(8)
	for target, ids := range buckets {
		slices.Sort(ids)
		ids = slices.Compact(ids)
		var bodies []any
		path := "on-conversation-updated"
		policy := "keep_queued"
		if system || kind == "adminless-reminder" {
			policy = "drop_if_unsupported"
			path = "on-conversation-" + kind
			if system {
				path = "on-conversation-system-" + kind
			}
			body := map[string]any{"time": now, "conversation": conv.ID, "already_present_users": ids}
			if kind == "member-update" {
				body["update"] = data
			}
			if kind == "adminless-reminder" {
				body["reminder"] = data
				if !system {
					body["orig_user_id"] = actorID
				}
			}
			bodies = []any{body}
		} else {
			modern := map[string]any{"time": now, "origUserId": actorID, "convId": conv.ID, "alreadyPresentUsers": ids, "action": action, "extraConversationData": map[string]any{"group_id": nil}}
			legacy := map[string]any{"cuTime": now, "cuOrigUserId": actorID, "cuConvId": conv.ID, "cuAlreadyPresentUsers": ids, "cuAction": action}
			bodies = []any{modern, legacy}
		}
		bundle := payloadBundle{Policy: stringPtr(policy)}
		for i, body := range bodies {
			encoded, err := json.Marshal(body)
			if err != nil {
				return errors.New("adminless notification encoding failed")
			}
			versions := &versionRange{From: intPtr(4)}
			if len(bodies) == 2 {
				versions = &versionRange{From: intPtr(1)}
				if i == 1 {
					versions = &versionRange{From: intPtr(0), Until: intPtr(1)}
				}
			}
			bundle.Notifications = append(bundle.Notifications, notification{Origin: stringPtr(domain), Component: stringPtr("galley"), Path: stringPtr(path), Body: stringPtr(string(encoded)), Versions: versions, RequestID: stringPtr(requestID)})
		}
		remote.Go(func() error {
			for attempt := 0; ; attempt++ {
				if err := n.Base.Queue.Enqueue(remoteCtx, target, bundle); err == nil {
					return nil
				}
				if remoteCtx.Err() != nil {
					return remoteCtx.Err()
				}
				if attempt == 3 {
					return errors.New("adminless notification enqueue failed")
				}
				if !wait(remoteCtx, time.Second) {
					return remoteCtx.Err()
				}
			}
		})
	}
	if err := remote.Wait(); err != nil {
		return err
	}
	// Source system Cells=true; user reminders are never Cells events. Clone the
	// routing metadata to reuse HTTP chunking without mutating the stored snapshot.
	routed := *conv
	routed.Metadata = map[string]any{}
	for key, value := range conv.Metadata {
		routed.Metadata[key] = value
	}
	if system {
		routed.Metadata["cells_state"] = int32(1)
	} else if kind == "adminless-reminder" {
		routed.Metadata["cells_state"] = int32(0)
	}
	join := groupJoin{Conversation: &routed, RequestID: requestID, Actor: actorID, Locals: local}
	if system {
		join.Actor.Domain = ""
	}
	if err := n.Base.pushLocal(ctx, join, event); err != nil {
		return err
	}
	if len(bots) > 0 {
		n.Base.Bots.DeliverAsync(ctx, bots, event)
	}
	return nil
}
