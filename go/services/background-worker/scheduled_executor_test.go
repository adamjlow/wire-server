// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

type scheduledExecutionFixture struct {
	mu          sync.Mutex
	beat        scheduledHeartbeat
	calls       []string
	gate        chan struct{}
	delay       int64
	completeErr error
	diagnostic  string
}

func (f *scheduledExecutionFixture) note(value string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, value)
}
func (f *scheduledExecutionFixture) CompleteScheduledJob(ctx context.Context, _ string, _ claimedScheduledJob, handler func(context.Context) error) error {
	f.note("begin")
	if err := handler(ctx); err != nil {
		f.note("rollback")
		return err
	}
	if f.completeErr != nil {
		return f.completeErr
	}
	f.note("ack")
	f.note("commit")
	return nil
}
func (f *scheduledExecutionFixture) HeartbeatScheduledJob(context.Context, string, int64, int32, time.Duration) (scheduledHeartbeat, error) {
	f.note("heartbeat")
	if f.gate != nil {
		select {
		case f.gate <- struct{}{}:
		default:
		}
	}
	return f.beat, nil
}
func (f *scheduledExecutionFixture) FailScheduledJob(_ context.Context, _ string, _ claimedScheduledJob, delay int64, diagnostic string) (bool, error) {
	f.note("fail")
	f.delay = delay
	f.diagnostic = diagnostic
	return true, nil
}
func (f *scheduledExecutionFixture) DeleteCancelledScheduledJob(context.Context, string, int64) error {
	f.note("cancel")
	return nil
}
func fixtureScheduledExecutor(f *scheduledExecutionFixture) scheduledExecutor {
	return scheduledExecutor{Store: f, Queue: "meetings", Heartbeat: time.Millisecond, Visibility: time.Minute, BackoffCap: 300 * time.Second, BackoffBase: 2, Jitter: "none"}
}
func TestScheduledExecutorSingleJobControlAndFailureBoundaries(t *testing.T) {
	for _, status := range []string{"success", "retry", "dlq", "reclaimed"} {
		t.Run(status, func(t *testing.T) {
			f := &scheduledExecutionFixture{}
			executor := fixtureScheduledExecutor(f)
			executor.Heartbeat = time.Hour
			job := claimedScheduledJob{ID: 1, Attempts: 1}
			if status == "dlq" {
				job.Attempts = 10
				executor.Jitter = "full"
				executor.Draw = func(float64, float64) float64 { t.Error("exhausted job consumed a jitter draw"); return 0 }
			}
			if status == "reclaimed" {
				f.completeErr = errScheduledGone
			}
			result := executor.Run(t.Context(), job, nil, func(context.Context) error {
				f.note("handler")
				if status == "retry" || status == "dlq" {
					return errors.New("synthetic private failure")
				}
				return nil
			})
			if result.Err != nil || result.Status != status {
				t.Fatal(result)
			}
			if (status == "retry" || status == "dlq") && f.diagnostic != "synthetic private failure" {
				t.Fatal("handler diagnostic was replaced before retry/DLQ")
			}
			want := []string{"begin", "handler", "ack", "commit"}
			if status == "retry" || status == "dlq" {
				want = []string{"begin", "handler", "rollback", "fail"}
			}
			if status == "reclaimed" {
				want = []string{"begin", "handler"}
			}
			if !reflect.DeepEqual(f.calls, want) {
				t.Fatal("control/failure boundary changed", f.calls)
			}
			if status == "retry" && f.delay != 2 {
				t.Fatal("backoff attempts not one-indexed")
			}
		})
	}
}
func TestScheduledExecutorLeaseCancellationAndShutdown(t *testing.T) {
	for _, status := range []string{"reclaimed", "cancelled", "shutdown", "gone-ignored"} {
		t.Run(status, func(t *testing.T) {
			f := &scheduledExecutionFixture{gate: make(chan struct{}, 1)}
			if status == "reclaimed" {
				f.beat.CurrentAttempts = ptr(int32(2))
			}
			if status == "cancelled" {
				f.beat.CancelRequested = true
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			executor := fixtureScheduledExecutor(f)
			result := executor.Run(ctx, claimedScheduledJob{ID: 1, Attempts: 1}, nil, func(jobCtx context.Context) error {
				if status == "gone-ignored" {
					select {
					case <-f.gate:
						return nil
					case <-time.After(time.Second):
						return errors.New("heartbeat absent")
					}
				}
				if status == "shutdown" {
					cancel()
				}
				<-jobCtx.Done()
				return jobCtx.Err()
			})
			want := status
			if status == "gone-ignored" {
				want = "success"
			}
			if result.Status != want {
				t.Fatal(result)
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			for _, call := range f.calls {
				if call == "fail" {
					t.Fatal("cancel/reclaim/shutdown recorded a failure")
				}
			}
		})
	}
}
func TestScheduledRetrySourcePicoJitterAndCeiling(t *testing.T) {
	for _, test := range []struct {
		base     float64
		attempts int32
		jitter   string
		draw     float64
		want     int64
	}{
		{2, 1, "none", 0, 2}, {2, 10, "none", 0, 300}, {.5, 1, "none", 0, 1}, {2, 1, "full", 0, 0}, {2, 1, "full", .5, 1}, {2, 1, "equal", 0, 1}, {2, 1, "equal", 1, 2}, {1.5, 3, "none", 0, 4},
	} {
		got, err := scheduledRetrySeconds(test.base, 300*time.Second, test.jitter, test.attempts, func(low, high float64) float64 { return low + test.draw*(high-low) })
		if err != nil || got != test.want {
			t.Fatal("native backoff changed", got, err, test)
		}
	}
}
