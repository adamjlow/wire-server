// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"errors"
	"time"
)

type scheduledExecutionStore interface {
	CompleteScheduledJob(context.Context, string, claimedScheduledJob, func(context.Context) error) error
	HeartbeatScheduledJob(context.Context, string, int64, int32, time.Duration) (scheduledHeartbeat, error)
	FailScheduledJob(context.Context, string, claimedScheduledJob, int64, string) (bool, error)
	DeleteCancelledScheduledJob(context.Context, string, int64) error
}
type scheduledExecutor struct {
	Store                             scheduledExecutionStore
	Queue                             string
	Heartbeat, Visibility, BackoffCap time.Duration
	BackoffBase                       float64
	Jitter                            string
	Draw                              func(float64, float64) float64
	Pulse                             func()
}
type scheduledExecutionOutcome struct {
	Status string
	Err    error
}

// SingleJobMode's action races an infrastructure heartbeat. DB heartbeat errors
// retry after3s then the regular interval; gone rows are harmless, reclaimed
// attempts stop the action without retry and force cancellation deletes only a
// currently cancel-marked row. Shutdown leaves its lease for durable reclaim.
func (e scheduledExecutor) Run(ctx context.Context, job claimedScheduledJob, force <-chan struct{}, handler func(context.Context) error) scheduledExecutionOutcome {
	if e.Store == nil || handler == nil || e.Heartbeat <= 0 || e.Visibility <= 0 {
		return scheduledExecutionOutcome{"error", errors.New("scheduled execution unavailable")}
	}
	if _, err := renderScheduledSQL("{{queue}}", e.Queue); err != nil {
		return scheduledExecutionOutcome{"error", err}
	}
	if ctx.Err() != nil {
		return scheduledExecutionOutcome{"shutdown", ctx.Err()}
	}
	jobCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		for wait(jobCtx, e.Heartbeat) {
			beat, err := e.Store.HeartbeatScheduledJob(jobCtx, e.Queue, job.ID, job.Attempts, e.Visibility)
			if err != nil {
				if !wait(jobCtx, 3*time.Second) {
					return
				}
				continue
			}
			if e.Pulse != nil {
				e.Pulse()
			}
			if beat.CancelRequested {
				cancel(errScheduledForceCancel)
				return
			}
			if !beat.Heartbeated && beat.CurrentAttempts != nil {
				cancel(errScheduledGone)
				return
			}
		}
	}()
	watcherDone := make(chan struct{})
	go func() {
		defer close(watcherDone)
		select {
		case <-force:
			cancel(errScheduledForceCancel)
		case <-jobCtx.Done():
		}
	}()
	// Go handlers use explicit context cancellation at backend boundaries. Recover
	// synchronous panics into sanitized retryable failures, without exposing values.
	run := func(ctx context.Context) (err error) {
		defer func() {
			if recover() != nil {
				err = errors.New("scheduled handler panicked")
			}
		}()
		return handler(ctx)
	}
	err := e.Store.CompleteScheduledJob(jobCtx, e.Queue, job, run)
	cause := context.Cause(jobCtx)
	cancel(nil)
	<-heartbeatDone
	<-watcherDone
	if err == nil {
		return scheduledExecutionOutcome{"success", nil}
	}
	if ctx.Err() != nil {
		return scheduledExecutionOutcome{"shutdown", ctx.Err()}
	}
	if errors.Is(cause, errScheduledForceCancel) {
		err = e.Store.DeleteCancelledScheduledJob(ctx, e.Queue, job.ID)
		return scheduledExecutionOutcome{"cancelled", err}
	}
	if errors.Is(cause, errScheduledGone) || errors.Is(err, errScheduledGone) {
		return scheduledExecutionOutcome{"reclaimed", nil}
	}
	maxAttempts := int32(10)
	if job.MaxAttempts != nil {
		maxAttempts = *job.MaxAttempts
	}
	delay := int64(0)
	if job.Attempts < maxAttempts {
		var delayErr error
		delay, delayErr = scheduledRetrySeconds(e.BackoffBase, e.BackoffCap, e.Jitter, job.Attempts, e.Draw)
		if delayErr != nil {
			return scheduledExecutionOutcome{"error", delayErr}
		}
	}
	changed, failureErr := e.Store.FailScheduledJob(ctx, e.Queue, job, delay, err.Error())
	if failureErr != nil {
		return scheduledExecutionOutcome{"error", failureErr}
	}
	if !changed {
		return scheduledExecutionOutcome{"reclaimed", nil}
	}

	if job.Attempts >= maxAttempts {
		return scheduledExecutionOutcome{"dlq", nil}
	}
	return scheduledExecutionOutcome{"retry", nil}
}
