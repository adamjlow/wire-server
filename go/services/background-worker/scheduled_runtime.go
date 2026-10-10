// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Native notification enqueue waits at most one second for the shared Rabbit
// channel; reconnect runs independently. Retry stays with the domain notifier.
type scheduledNotificationQueue struct {
	Service *Service
	mu      sync.Mutex
	channel *amqp.Channel
	ready   chan struct{}
}

func (q *scheduledNotificationQueue) Run(ctx context.Context) {
	for ctx.Err() == nil {
		conn, err := q.Service.openRabbit(ctx, "scheduled-job-notifications")
		if err == nil {
			stopConnection := context.AfterFunc(ctx, func() { _ = conn.CloseDeadline(time.Now()) })
			channel, err := conn.Channel()
			if err == nil {
				closed := channel.NotifyClose(make(chan *amqp.Error, 1))
				q.mu.Lock()
				q.channel = channel
				close(q.ready)
				q.mu.Unlock()
				select {
				case <-ctx.Done():
				case <-closed:
				}
				q.mu.Lock()
				q.channel = nil
				q.ready = make(chan struct{})
				q.mu.Unlock()
			}
			_ = conn.CloseDeadline(time.Now().Add(time.Second))
			stopConnection()
		}
		if !wait(ctx, time.Second) {
			return
		}
	}
}
func (q *scheduledNotificationQueue) Enqueue(ctx context.Context, domain string, bundle payloadBundle) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for {
		q.mu.Lock()
		channel, ready := q.channel, q.ready
		q.mu.Unlock()
		if channel != nil {
			return (rabbitJobPublisher{Channel: channel}).Enqueue(ctx, domain, bundle)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return errors.New("scheduled notification channel unavailable")
		case <-ready:
		}
	}
}

// Capability lookup has the domain's two-argument interface; scope its outbound
// Request-Id to this job rather than storing mutable state on the shared notifier.
type adminlessJobNotifications struct {
	*adminlessNotifier
	RequestID string
}

func (n adminlessJobNotifications) SupportsSystemDelete(ctx context.Context, target string) (bool, error) {
	return n.adminlessNotifier.SupportsSystemDelete(ctx, target, n.RequestID)
}

type scheduledRuntime struct {
	Service       *Service
	Postgres      *postgresStore
	Pools         []*scheduledPool
	Queue         *scheduledNotificationQueue
	listenerReady atomic.Bool
}

// Bind the existing domain adapters without an extra storage abstraction or new
// transaction boundary. Each account lookup carries its own job's Request-Id.
func (s *Service) scheduledPools(pg *postgresStore, cass cassandraConversationStore, bots botEventDelivery, queue notificationEnqueuer, ordinary, federation *http.Client) ([]*scheduledPool, error) {
	if pg == nil || ordinary == nil || federation == nil || queue == nil {
		return nil, errors.New("scheduled dependencies unavailable")
	}
	defaults, err := s.Galley.adminlessDefaults()
	if err != nil {
		return nil, err
	}
	read := &routedGroupStore{Groups: pg, Postgres: pg, Cassandra: cass, Location: s.Config.Storage.Conversation}
	removalData := scheduledRemovalStore{Postgres: pg, Cassandra: cass, Location: s.Config.Storage.Conversation, Codes: s.Config.Storage.Codes}
	removal := scheduledConversationRemoval{Read: read, State: removalData}
	features := adminlessFeatureStore{Postgres: pg, Cassandra: cass, Location: s.Config.Storage.TeamFeatures, Defaults: defaults}
	notifier := &adminlessNotifier{Base: conversationJoinNotifier{Queue: queue, Bots: bots, Client: ordinary, Gundeck: s.Config.Gundeck, Domain: s.Galley.Settings.Domain, Now: time.Now}, Federation: s, FederationClient: federation}
	meetings := func(ctx context.Context, payload scheduledJobPayload) error {
		if payload.Type != "meetings_cleanup" {
			return errors.New("invalid meetings job")
		}
		result, err := runConfiguredMeetingsCleanup(ctx, s.Config.Meetings, routedMeetingsCleanupStore{Postgres: pg, Codes: s.Config.Storage.Codes}, removal, time.Now(), func(_, _ int64, err error) {
			if err != nil {
				slog.Error("meetings cleanup batch failed")
			}
		})
		if err == nil && result.Completed {
			s.mu.Lock()
			s.meetingsCleanupRuns++
			s.mu.Unlock()
		}
		return err
	}
	conversations := func(ctx context.Context, payload scheduledJobPayload) error {
		accounts := adminlessBrigAccounts{Endpoint: s.Config.Brig, Client: ordinary, RequestID: payload.RequestID}
		store := adminlessConversationStore{Read: read, Data: removalData, Domain: s.Galley.Settings.Domain, FeatureLookup: features.Feature, UserLookup: accounts.Users}
		dispatcher := adminlessDispatcher{Store: store, Notify: adminlessJobNotifications{adminlessNotifier: notifier, RequestID: payload.RequestID}, Schedule: adminlessDurableScheduler{Store: pg, Now: time.Now}, Removal: removal, Domain: s.Galley.Settings.Domain}
		return dispatcher.Dispatch(ctx, payload)
	}
	pools := make([]*scheduledPool, 0, 2)
	for _, item := range []struct {
		queue   string
		handler func(context.Context, scheduledJobPayload) error
	}{{"meetings", meetings}, {"conversations", conversations}} {
		id, err := newJobID()
		if err != nil {
			return nil, err
		}
		pool, err := newScheduledPool(pg, item.queue, id, s.Config, item.handler)
		if err != nil {
			return nil, err
		}
		pool.Report = func(event string, outcome scheduledExecutionOutcome) {
			if outcome.Err != nil {
				slog.Error("scheduled worker operation failed", "queue", item.queue, "operation", event, "status", outcome.Status)
			}
			if outcome.Status == "timeout" {
				slog.Warn("scheduled worker drain timed out", "queue", item.queue)
			}
		}
		pools = append(pools, pool)
	}
	return pools, nil
}

// Both pool drains retain cron, reapers, NOTIFY and registry/job heartbeats.
// Cancellation of support threads happens after both native drains complete.
func (r *scheduledRuntime) Run(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	supportCtx, stopSupport := context.WithCancel(context.WithoutCancel(ctx))
	defer stopSupport()
	var support, pools sync.WaitGroup
	if r.Queue != nil {
		support.Go(func() { r.Queue.Run(supportCtx) })
	}
	support.Go(func() {
		runScheduledListener(supportCtx, r.Postgres.Pool.Config().ConnConfig, r.Pools, r.listenerReady.Store, func(error) { slog.Warn("scheduled listener reconnecting") })
	})
	support.Go(func() {
		for supportCtx.Err() == nil {
			err := r.Postgres.runMeetingsCron(supportCtx, r.Service.Config.Meetings.Schedule, r.Pools[0].cronWake, func(result cronSchedulerReport) {
				if result.Manual != nil && result.Manual.Err != nil || result.Tick != nil && (result.Tick.Err != nil || result.Tick.InsertError != nil) {
					slog.Error("scheduled cron operation failed")
				}
			})
			if supportCtx.Err() != nil {
				return
			}
			if err != nil {
				slog.Error("scheduled cron scheduler failed")
			}
			if !wait(supportCtx, 2*time.Second) {
				return
			}
		}
	})
	for range r.Pools {
		// Each native pool owns a reaper, with shared database gates preventing
		// duplicate effects. Keep both lifecycles and their immediate first pass.
		support.Go(func() { r.runReaper(supportCtx) })
	}
	for _, pool := range r.Pools {
		pools.Go(func() {
			if err := pool.Run(ctx); err != nil {
				slog.Error("scheduled pool stopped")
			}
		})
	}
	pools.Wait()
	stopSupport()
	support.Wait()
}
func (r *scheduledRuntime) runReaper(ctx context.Context) {
	interval := time.Duration(r.Service.Config.Jobs.ReaperInterval)
	timeout := time.Duration(r.Service.Config.Jobs.ReaperTimeout)
	cadence := interval
	if rem := interval % time.Second; rem != 0 {
		extra := time.Second - rem
		if interval > time.Duration(math.MaxInt64)-extra {
			cadence = time.Duration(math.MaxInt64)
		} else {
			cadence += extra
		}
	}
	for ctx.Err() == nil {
		for _, result := range r.Postgres.reapScheduledJobs(ctx, interval, timeout) {
			if result.Err != nil || len(result.FailedQueues) > 0 {
				slog.Error("scheduled reaper operation failed", "operation", result.Task)
			}
		}
		if !wait(ctx, cadence) {
			return
		}
	}
}
