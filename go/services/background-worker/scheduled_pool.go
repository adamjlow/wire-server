// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"time"
)

type scheduledPoolStore interface {
	scheduledExecutionStore
	ClaimScheduledJobs(context.Context, string, string, time.Duration, int) ([]claimedScheduledJob, error)
	RegisterScheduledWorker(context.Context, string, string, *string, int, time.Duration) (bool, error)
	HeartbeatScheduledWorker(context.Context, string) (bool, bool, error)
	scheduledWorkerState(context.Context, string, bool) error
}
type scheduledPool struct {
	Store                                                   scheduledPoolStore
	Queue, WorkerID, Liveness                               string
	Host                                                    *string
	Threads                                                 int
	Poll, RegistryHeartbeat, Stale                          time.Duration
	Grace                                                   *time.Duration
	Executor                                                scheduledExecutor
	Handler                                                 func(context.Context, scheduledJobPayload) error
	Report                                                  func(string, scheduledExecutionOutcome)
	mu                                                      sync.Mutex
	paused, shutting                                        bool
	outstanding                                             int
	running                                                 map[int64]chan struct{}
	wake, proof, drained, cronWake, registryWake, stateWake chan struct{}
}

func newScheduledPool(store scheduledPoolStore, queue, workerID string, c Config, handler func(context.Context, scheduledJobPayload) error) (*scheduledPool, error) {
	if store == nil || handler == nil || c.Jobs.Threads < 1 || c.Jobs.Threads > 1000 || c.Jobs.Poll <= 0 || c.Jobs.WorkerHeartbeat <= 0 || c.Jobs.WorkerStale <= 0 || c.Jobs.JobHeartbeat <= 0 || c.Jobs.Visibility <= 0 {
		return nil, errors.New("invalid scheduled pool")
	}
	if _, err := renderScheduledSQL("{{queue}}", queue); err != nil {
		return nil, err
	}
	id, err := parseUserID(workerID)
	if err != nil {
		return nil, errors.New("invalid scheduled worker identifier")
	}
	if _, err := scheduledRetrySeconds(c.Jobs.BackoffBase, time.Duration(c.Jobs.BackoffCap), c.Jobs.Jitter, 0, func(float64, float64) float64 { return 0 }); err != nil {
		return nil, err
	}
	host, err := os.Hostname()
	if err != nil {
		return nil, errors.New("scheduled hostname unavailable")
	}
	p := &scheduledPool{Store: store, Queue: queue, WorkerID: id.String(), Host: &host, Threads: c.Jobs.Threads, Poll: time.Duration(c.Jobs.Poll), RegistryHeartbeat: time.Duration(c.Jobs.WorkerHeartbeat), Stale: time.Duration(c.Jobs.WorkerStale), Handler: handler, running: map[int64]chan struct{}{}, wake: make(chan struct{}, 1), proof: make(chan struct{}, 1), drained: make(chan struct{}, 1), cronWake: make(chan struct{}, 1), registryWake: make(chan struct{}, 1), stateWake: make(chan struct{}, 1)}
	if c.Jobs.Grace != nil {
		grace := time.Duration(*c.Jobs.Grace)
		if grace <= 0 {
			return nil, errors.New("invalid scheduled grace")
		}
		p.Grace = &grace
	}
	p.Liveness = os.TempDir() + "/arbiter-worker-" + p.WorkerID
	p.Executor = scheduledExecutor{Store: store, Queue: queue, Heartbeat: time.Duration(c.Jobs.JobHeartbeat), Visibility: time.Duration(c.Jobs.Visibility), BackoffBase: c.Jobs.BackoffBase, BackoffCap: time.Duration(c.Jobs.BackoffCap), Jitter: c.Jobs.Jitter, Pulse: p.pulse}
	return p, nil
}
func signalScheduled(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}
func (p *scheduledPool) pulse() { signalScheduled(p.proof) }
func (p *scheduledPool) report(event string, result scheduledExecutionOutcome) {
	if p.Report != nil {
		p.Report(event, result)
	}
}
func (p *scheduledPool) setPause(value bool) {
	p.mu.Lock()
	changed := !p.shutting && p.paused != value
	if changed {
		p.paused = value
	}
	p.mu.Unlock()
	if changed {
		signalScheduled(p.registryWake)
		signalScheduled(p.stateWake)
	}
}

func (p *scheduledPool) Notify(channel, payload string) {
	if channel == p.Queue+"_created" {
		signalScheduled(p.wake)
		return
	}
	if channel == "arbiter_cron_run_arbiter" && p.Queue == "meetings" && payload == "meetings-cleanup" {
		signalScheduled(p.cronWake)
		return
	}
	if channel != "arbiter_pause_arbiter_"+p.Queue && channel != "arbiter_cancel_arbiter_"+p.Queue {
		return
	}
	if !validAdminlessAccountJSONText([]byte(payload)) {
		return
	}
	var object map[string]json.RawMessage
	if json.Unmarshal([]byte(payload), &object) != nil || object == nil {
		return
	}
	var id string
	if json.Unmarshal(object["worker_id"], &id) != nil {
		return
	}
	parsed, err := parseUserID(id)
	if err != nil || parsed.String() != p.WorkerID {
		return
	}
	if channel == "arbiter_pause_arbiter_"+p.Queue {
		var paused *bool
		if json.Unmarshal(object["paused"], &paused) != nil || paused == nil {
			return
		}
		p.setPause(*paused)
		return
	}
	job, ok := adminlessVersionInteger(object["job_id"])
	if !ok {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if force := p.running[job]; force != nil {
		close(force)
		delete(p.running, job)
	}
}
func (p *scheduledPool) register(ctx context.Context, initial bool) {
	paused, err := p.Store.RegisterScheduledWorker(ctx, p.Queue, p.WorkerID, p.Host, p.Threads, p.Stale)
	if err == nil {
		// Native re-registration restores the row but reconciles pause only on
		// the following heartbeat. Initial registration applies pause immediately.
		if initial {
			p.setPause(paused)
		}
	} else {
		p.report("register", scheduledExecutionOutcome{"error", err})
	}
}
func (p *scheduledPool) registry(ctx context.Context) {
	tick := func() {
		if err := os.WriteFile(p.Liveness, nil, 0644); err != nil {
			p.report("liveness", scheduledExecutionOutcome{"error", errors.New("scheduled liveness write failed")})
		}
		paused, found, err := p.Store.HeartbeatScheduledWorker(ctx, p.WorkerID)
		if err != nil {
			p.report("registry", scheduledExecutionOutcome{"error", err})
			return
		}
		defer p.report("registry", scheduledExecutionOutcome{"success", nil})
		if found {
			p.setPause(paused)
			return
		}
		p.mu.Lock()
		shutting := p.shutting
		p.mu.Unlock()
		if !shutting {
			p.register(ctx, false)
		}
	}
	tick()
	for wait(ctx, p.RegistryHeartbeat) {
		// Cadence must elapse before consuming a coalesced proof-of-work pulse.
		// Paused pools need no pulse, so operators can still resume an idle pool.
		for {
			p.mu.Lock()
			paused := p.paused
			p.mu.Unlock()
			if paused {
				break
			}
			select {
			case <-ctx.Done():
				return
			case <-p.proof:
				goto ready
			case <-p.registryWake:
			}
		}
	ready:
		tick()
	}
}
func (p *scheduledPool) dispatch(ctx context.Context, work chan<- claimedScheduledJob) {
	for {
		p.mu.Lock()
		paused, free, shutting := p.paused, p.Threads-p.outstanding, p.shutting
		p.mu.Unlock()
		if shutting || ctx.Err() != nil {
			return
		}
		if paused {
			select {
			case <-ctx.Done():
				return
			case <-p.stateWake:
			}
			continue
		}
		if free > 0 {
			jobs, err := p.Store.ClaimScheduledJobs(ctx, p.Queue, p.WorkerID, p.Executor.Visibility, free)
			p.pulse() // Even failed claims prove dispatcher liveness.
			if err != nil {
				p.report("claim", scheduledExecutionOutcome{"error", err})
			} else {
				p.mu.Lock()
				p.outstanding += len(jobs)
				p.mu.Unlock()
				// Capacity includes running and queued jobs, so committed claims fit even
				// if shutdown begins now. Drain them rather than discard a committed lease.
				for _, job := range jobs {
					work <- job
				}
			}
		}
		timer := time.NewTimer(p.Poll)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-p.wake:
			timer.Stop()
		case <-p.stateWake:
			timer.Stop()
		case <-timer.C:
		}
	}
}
func (p *scheduledPool) worker(ctx context.Context, work <-chan claimedScheduledJob) {
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-work:
			force := make(chan struct{})
			p.mu.Lock()
			p.running[job.ID] = force
			p.mu.Unlock()
			result := p.Executor.Run(ctx, job, force, func(ctx context.Context) error {
				payload, err := decodeScheduledPayload(p.Queue, job.Payload)
				if err != nil {
					return err
				}
				return p.Handler(ctx, payload)
			})
			p.mu.Lock()
			delete(p.running, job.ID)
			p.outstanding--
			p.mu.Unlock()
			p.report("job", result)
			signalScheduled(p.drained)
			signalScheduled(p.wake)
			if result.Status == "error" {
				if !wait(ctx, 2*time.Second) {
					return
				}
			}
		}
	}
}

// Parent cancellation initiates native drain rather than canceling running jobs.
// Registry and per-job leases remain live until drain completes or grace expires.
func (p *scheduledPool) Run(ctx context.Context) error {
	if ctx.Err() != nil {
		return nil
	}
	jobsCtx, cancelJobs := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelJobs()
	defer func() { _ = os.Remove(p.Liveness) }()
	p.register(ctx, true)
	work := make(chan claimedScheduledJob, p.Threads)
	var workers sync.WaitGroup
	for i := 0; i < p.Threads; i++ {
		workers.Go(func() { p.worker(jobsCtx, work) })
	}
	registryDone := make(chan struct{})
	go func() { defer close(registryDone); p.registry(jobsCtx) }()
	dispatchDone := make(chan struct{})
	go func() { defer close(dispatchDone); p.dispatch(ctx, work) }()
	<-ctx.Done()
	p.mu.Lock()
	p.shutting = true
	p.mu.Unlock()
	signalScheduled(p.wake)
	if err := p.Store.scheduledWorkerState(jobsCtx, p.WorkerID, false); err != nil {
		p.report("shutdown-mark", scheduledExecutionOutcome{"error", err})
	}
	<-dispatchDone
	var deadline <-chan time.Time
	var timer *time.Timer
	if p.Grace != nil {
		timer = time.NewTimer(*p.Grace)
		deadline = timer.C
		defer timer.Stop()
	}
drain:
	for {
		p.mu.Lock()
		remaining := p.outstanding
		p.mu.Unlock()
		if remaining == 0 {
			break
		}
		select {
		case <-p.drained:
		case <-deadline:
			p.report("drain", scheduledExecutionOutcome{"timeout", nil})
			break drain
		}
	}
	if err := p.Store.scheduledWorkerState(jobsCtx, p.WorkerID, true); err != nil {
		p.report("deregister", scheduledExecutionOutcome{"error", err})
	}
	cancelJobs()
	workers.Wait()
	<-registryDone
	return nil
}
