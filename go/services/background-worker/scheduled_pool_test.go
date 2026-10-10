// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func scheduledPoolFixtureConfig() Config {
	c := Config{}
	c.Jobs.Threads = 2
	c.Jobs.Poll = Duration(time.Hour)
	c.Jobs.WorkerHeartbeat = Duration(time.Hour)
	c.Jobs.WorkerStale = Duration(5 * time.Hour)
	c.Jobs.JobHeartbeat = Duration(time.Hour)
	c.Jobs.Visibility = Duration(2 * time.Hour)
	c.Jobs.BackoffBase = 2
	c.Jobs.BackoffCap = Duration(time.Minute)
	c.Jobs.Jitter = "none"
	return c
}

type observedScheduledPoolStore struct {
	scheduledExecutionFixture
	stateMu                                sync.Mutex
	paused                                 bool
	backlog                                []claimedScheduledJob
	registered, heartbeat, marked, removed chan struct{}
}

func (f *observedScheduledPoolStore) RegisterScheduledWorker(context.Context, string, string, *string, int, time.Duration) (bool, error) {
	f.stateMu.Lock()
	defer f.stateMu.Unlock()
	signalScheduled(f.registered)
	return f.paused, nil
}
func (f *observedScheduledPoolStore) HeartbeatScheduledWorker(context.Context, string) (bool, bool, error) {
	f.stateMu.Lock()
	defer f.stateMu.Unlock()
	return f.paused, true, nil
}
func (f *observedScheduledPoolStore) scheduledWorkerState(_ context.Context, _ string, remove bool) error {
	if remove {
		signalScheduled(f.removed)
	} else {
		signalScheduled(f.marked)
	}
	return nil
}
func (f *observedScheduledPoolStore) ClaimScheduledJobs(_ context.Context, _, _ string, _ time.Duration, budget int) ([]claimedScheduledJob, error) {
	f.stateMu.Lock()
	defer f.stateMu.Unlock()
	size := min(budget, len(f.backlog))
	result := append([]claimedScheduledJob{}, f.backlog[:size]...)
	f.backlog = f.backlog[size:]
	return result, nil
}
func awaitScheduledTestSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("scheduled test signal absent")
	}
}
func TestScheduledPoolCapacityPauseAndUnlimitedDrain(t *testing.T) {
	f := &observedScheduledPoolStore{paused: true, registered: make(chan struct{}, 1), heartbeat: make(chan struct{}, 1), marked: make(chan struct{}, 1), removed: make(chan struct{}, 1)}
	for id := int64(1); id <= 3; id++ {
		f.backlog = append(f.backlog, claimedScheduledJob{ID: id, Attempts: 1, Payload: json.RawMessage(`{"type":"meetings_cleanup","data":{}}`)})
	}
	started := make(chan struct{}, 3)
	release := make(chan struct{})
	completed := make(chan struct{}, 3)
	p, err := newScheduledPool(f, "meetings", jobUUID, scheduledPoolFixtureConfig(), func(ctx context.Context, _ scheduledJobPayload) error {
		started <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
			return ctx.Err()
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	p.Liveness = filepath.Join(t.TempDir(), "liveness")
	p.Report = func(event string, _ scheduledExecutionOutcome) {
		if event == "registry" {
			signalScheduled(f.heartbeat)
		}
		if event == "job" {
			completed <- struct{}{}
		}
	}
	ctx, stop := context.WithCancel(t.Context())
	defer stop()
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()
	defer func() {
		stop()
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	awaitScheduledTestSignal(t, f.registered)
	awaitScheduledTestSignal(t, f.heartbeat)
	f.stateMu.Lock()
	remaining := len(f.backlog)
	f.paused = false
	f.stateMu.Unlock()
	if remaining != 3 {
		t.Fatal("paused startup claimed work")
	}
	p.Notify("arbiter_pause_arbiter_meetings", `{"worker_id":"`+jobUUID+`","paused":false}`)
	awaitScheduledTestSignal(t, started)
	awaitScheduledTestSignal(t, started)
	p.mu.Lock()
	outstanding := p.outstanding
	p.mu.Unlock()
	f.stateMu.Lock()
	remaining = len(f.backlog)
	f.stateMu.Unlock()
	if outstanding != 2 || remaining != 1 {
		t.Fatal("capacity includes queued and running work", outstanding, remaining)
	}
	stop()
	awaitScheduledTestSignal(t, f.marked)
	// Native drain retains live handler contexts and must not claim the third job.
	close(release)
	awaitScheduledTestSignal(t, completed)
	awaitScheduledTestSignal(t, completed)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("pool did not drain")
	}
	awaitScheduledTestSignal(t, f.removed)
	if _, err := os.Stat(p.Liveness); !os.IsNotExist(err) {
		t.Fatal("liveness survived completed pool")
	}
	f.stateMu.Lock()
	defer f.stateMu.Unlock()
	if len(f.backlog) != 1 {
		t.Fatal("shutdown claimed more work")
	}
}
func TestScheduledPoolNotificationOwnershipAndRequiredSchemas(t *testing.T) {
	f := &observedScheduledPoolStore{}
	p, err := newScheduledPool(f, "meetings", jobUUID, scheduledPoolFixtureConfig(), func(context.Context, scheduledJobPayload) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	force := make(chan struct{})
	p.running[1] = force
	for _, payload := range []string{`{"worker_id":"00000000-0000-0000-0000-000000000002","job_id":1}`, `{"worker_id":"` + jobUUID + `","job_id":1.5}`, `{"worker_id":"` + jobUUID + `","job_id":null}`} {
		p.Notify("arbiter_cancel_arbiter_meetings", payload)
	}
	select {
	case <-force:
		t.Fatal("foreign/malformed force cancel accepted")
	default:
	}
	p.Notify("arbiter_cancel_arbiter_meetings", `{"worker_id":"`+jobUUID+`","job_id":1.0}`)
	awaitScheduledTestSignal(t, force)
	p.Notify("arbiter_cancel_arbiter_meetings", `{"worker_id":"`+jobUUID+`","job_id":1}`)
	p.Notify("arbiter_pause_arbiter_meetings", `{"worker_id":"`+jobUUID+`","paused":null}`)
	if p.paused {
		t.Fatal("null required pause accepted")
	}
	p.Notify("arbiter_pause_arbiter_meetings", `{"worker_id":"`+jobUUID+`","paused":true}`)
	if !p.paused {
		t.Fatal("owned pause ignored")
	}
	p.shutting = true
	p.Notify("arbiter_pause_arbiter_meetings", `{"worker_id":"`+jobUUID+`","paused":false}`)
	if !p.paused {
		t.Fatal("shutdown pause state mutated")
	}
	p.Notify("arbiter_cron_run_arbiter", "unowned")
	select {
	case <-p.cronWake:
		t.Fatal("unowned cron wake")
	default:
	}
	p.Notify("arbiter_cron_run_arbiter", "meetings-cleanup")
	awaitScheduledTestSignal(t, p.cronWake)
}
