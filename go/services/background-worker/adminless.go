// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"math/big"
	"sort"

	"golang.org/x/sync/errgroup"
)

type adminlessFeature struct {
	Enabled      bool
	Strategy     string
	DeletionPico *big.Int
	ReminderPico []*big.Int
}
type adminlessUser struct {
	ID, Name, Type, Status string
	Service                bool
}
type adminlessCandidate struct {
	User qualifiedUser
	Name string
}

// Source dependencies are explicit: feature lookup, Brig account eligibility,
// durable scheduling and user/system notification effects. Runtime adapters must
// implement every method; absence is an error, never an acknowledged no-op.
type adminlessStore interface {
	Conversation(context.Context, string) (*storedConversation, error)
	TeamConversations(context.Context, string) ([]string, error)
	Feature(context.Context, string) (adminlessFeature, error)
	Users(context.Context, []string) ([]adminlessUser, error)
	Promote(context.Context, *storedConversation, qualifiedUser) error
}
type adminlessNotifications interface {
	SupportsSystemDelete(context.Context, string) (bool, error)
	Promoted(context.Context, *storedConversation, qualifiedUser, *string, string) error
	Deleted(context.Context, *storedConversation, *string, string) error
	Reminder(context.Context, *storedConversation, *string, string, string) error
}
type adminlessScheduler interface {
	Schedule(context.Context, string, *storedConversation, *string, string, adminlessFeature) error
}
type adminlessDispatcher struct {
	Store    adminlessStore
	Notify   adminlessNotifications
	Schedule adminlessScheduler
	Removal  scheduledConversationRemoval
	Domain   string
	Seed     func() (uint64, error)
}

func adminlessRandomSeed() (uint64, error) {
	var bytes [8]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return 0, errors.New("adminless random source unavailable")
	}
	return binary.BigEndian.Uint64(bytes[:]), nil
}
func selectAdminlessCandidates(seed uint64, strategy string, candidates []adminlessCandidate) ([]qualifiedUser, error) {
	if len(candidates) == 0 {
		return nil, errors.New("no adminless promotion candidates")
	}
	sorted := append([]adminlessCandidate(nil), candidates...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Name != sorted[j].Name {
			return sorted[i].Name < sorted[j].Name
		}
		return sorted[i].User.ID < sorted[j].User.ID
	})
	switch strategy {
	case "alphabetical":
		return []qualifiedUser{sorted[0].User}, nil
	case "random":
		return []qualifiedUser{sorted[seed%uint64(len(sorted))].User}, nil
	case "all":
		result := make([]qualifiedUser, len(sorted))
		for i, c := range sorted {
			result[i] = c.User
		}
		return result, nil
	default:
		return nil, errors.New("invalid adminless promotion strategy")
	}
}
func adminlessConversationCandidate(conv *storedConversation) (bool, error) {
	typ, ok := conv.Metadata["type"].(int32)
	if !ok {
		return false, errors.New("invalid adminless conversation type")
	}
	if typ != 0 {
		return false, nil
	}
	group, ok := conv.Metadata["group_conv_type"].(*int32)
	if !ok && conv.Metadata["group_conv_type"] != nil {
		return false, errors.New("invalid adminless group type")
	}
	return group == nil || *group == 0 || *group == 1, nil
}
func (d adminlessDispatcher) tryPromote(ctx context.Context, id string, actor *string, request string, otherwise func(*storedConversation, adminlessFeature) error) error {
	conv, err := d.Store.Conversation(ctx, id)
	if err != nil {
		return err
	}
	if conv == nil {
		return errors.New("adminless conversation not found")
	}
	candidate, err := adminlessConversationCandidate(conv)
	if err != nil {
		return err
	}
	if !candidate || conv.Team == nil {
		return nil
	}
	feature, err := d.Store.Feature(ctx, *conv.Team)
	if err != nil {
		return err
	}
	admin := false
	for _, m := range conv.LocalMembers {
		admin = admin || m.Role == "wire_admin"
	}
	for _, m := range conv.RemoteMembers {
		admin = admin || m.Role == "wire_admin"
	}
	if !feature.Enabled || admin {
		return nil
	}
	// Source requests unique sorted local IDs; the account map is last-wins, then
	// candidates retain original conversation member order (including duplicates).
	ids := make([]string, 0, len(conv.LocalMembers))
	seen := map[string]bool{}
	for _, m := range conv.LocalMembers {
		if !seen[m.ID] {
			seen[m.ID] = true
			ids = append(ids, m.ID)
		}
	}
	sort.Strings(ids)
	users, err := d.Store.Users(ctx, ids)
	if err != nil {
		return err
	}
	byID := map[string]adminlessUser{}
	for _, u := range users {
		byID[u.ID] = u
	}
	var eligible []adminlessCandidate
	for _, m := range conv.LocalMembers {
		u, ok := byID[m.ID]
		if ok && u.Type == "regular" && u.Status == "active" && !u.Service {
			eligible = append(eligible, adminlessCandidate{User: qualifiedUser{ID: m.ID, Domain: d.Domain}, Name: u.Name})
		}
	}
	if len(eligible) == 0 {
		return otherwise(conv, feature)
	}
	seed := d.Seed
	if seed == nil {
		seed = adminlessRandomSeed
	}
	value, err := seed()
	if err != nil {
		return err
	}
	promoted, err := selectAdminlessCandidates(value, feature.Strategy, eligible)
	if err != nil {
		return err
	}
	for _, user := range promoted {
		if err := d.Store.Promote(ctx, conv, user); err != nil {
			return err
		}
		// Keep the original member snapshot for every notification, even when more
		// than one member is promoted; source does not refresh it within this loop.
		if err := d.Notify.Promoted(ctx, conv, user, actor, request); err != nil {
			return err
		}
	}
	return nil
}
func adminlessRemoteDomains(conv *storedConversation) []string {
	var domains []string
	seen := map[string]bool{}
	for _, m := range conv.RemoteMembers {
		if !seen[m.User.Domain] {
			seen[m.User.Domain] = true
			domains = append(domains, m.User.Domain)
		}
	}
	return domains
}
func (d adminlessDispatcher) systemDeleteSupported(ctx context.Context, conv *storedConversation, cache map[string]bool) (bool, error) {
	domains := adminlessRemoteDomains(conv)
	supported := make([]bool, len(domains))
	lookup := func(i int) error {
		value, found := cache[domains[i]]
		if !found {
			var err error
			value, err = d.Notify.SupportsSystemDelete(ctx, domains[i])
			// Native capability negotiation maps each federation failure to unsupported.
			// Cancellation still belongs to the worker lifecycle, not a cache result.
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err != nil {
				value = false
			}
			if cache != nil {
				cache[domains[i]] = value
			}
		}
		supported[i] = value
		return nil
	}
	if cache != nil {
		// Setup invokes one backend at a time, retaining positive and negative results.
		for i := range domains {
			if err := lookup(i); err != nil {
				return false, err
			}
		}
	} else {
		// Direct deletion uses native federation's eight-worker pooled traversal.
		group, _ := errgroup.WithContext(ctx)
		group.SetLimit(8)
		for i := range domains {
			group.Go(func() error { return lookup(i) })
		}
		if err := group.Wait(); err != nil {
			return false, err
		}
	}
	for _, value := range supported {
		if !value {
			return false, nil
		}
	}
	return true, nil
}
func (d adminlessDispatcher) Dispatch(ctx context.Context, job scheduledJobPayload) error {
	if d.Store == nil || d.Notify == nil || !validDomain(d.Domain) {
		return errors.New("adminless dependencies unavailable")
	}
	switch job.Type {
	case "adminless_setup":
		if d.Schedule == nil {
			return errors.New("adminless scheduler unavailable")
		}
		ids, err := d.Store.TeamConversations(ctx, job.Team)
		if err != nil {
			return err
		}
		cache := map[string]bool{}
		for _, id := range ids {
			err := d.tryPromote(ctx, id, job.Actor, job.RequestID, func(conv *storedConversation, feature adminlessFeature) error {
				if job.Actor == nil {
					supported, err := d.systemDeleteSupported(ctx, conv, cache)
					if err != nil {
						return err
					}
					if !supported {
						return nil
					}
				}
				return d.Schedule.Schedule(ctx, job.Team, conv, job.Actor, job.RequestID, feature)
			})
			if err != nil {
				return err
			}
		}
		return nil
	case "adminless_deletion":
		return d.tryPromote(ctx, job.Conversation, job.Actor, job.RequestID, func(conv *storedConversation, _ adminlessFeature) error {
			if job.Actor == nil && len(conv.RemoteMembers) > 0 {
				supported, err := d.systemDeleteSupported(ctx, conv, nil)
				if err != nil {
					return err
				}
				if !supported {
					return nil
				}
			}
			if err := d.Removal.removeSnapshot(ctx, conv); err != nil {
				return err
			}
			return d.Notify.Deleted(ctx, conv, job.Actor, job.RequestID)
		})
	case "adminless_reminder":
		timestamp := job.DeletionScheduledText
		if timestamp == nil && job.DeletionScheduledFor != nil {
			text := job.DeletionScheduledFor.UTC().Format("2006-01-02T15:04:05.000Z")
			timestamp = &text
		}
		if timestamp == nil {
			return errors.New("adminless deletion time unavailable")
		}
		return d.tryPromote(ctx, job.Conversation, job.Actor, job.RequestID, func(conv *storedConversation, _ adminlessFeature) error {
			return d.Notify.Reminder(ctx, conv, job.Actor, job.RequestID, *timestamp)
		})
	default:
		return errors.New("invalid adminless job type")
	}
}
