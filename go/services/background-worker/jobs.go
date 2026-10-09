// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	crand "crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const backgroundJobsQueue = "background-jobs"

type groupSyncPayload struct {
	TeamID      string  `json:"team_id"`
	UserGroupID string  `json:"user_group_id"`
	ConvID      string  `json:"conv_id,omitempty"`
	Actor       *string `json:"actor,omitempty"`
}

type backgroundJobPayload struct {
	Type    string           `json:"type"`
	Payload groupSyncPayload `json:"payload"`
}

type backgroundJob struct {
	ID        string               `json:"id"`
	RequestID string               `json:"requestId"`
	Payload   backgroundJobPayload `json:"payload"`
}

// Required fields need pointers during decoding: zero values must not turn
// missing/null source-schema fields into a valid job. Unknown fields remain
// accepted, as in the source Schema decoder.
func decodeBackgroundJob(body []byte) (backgroundJob, error) {
	var raw struct {
		ID        *string `json:"id"`
		RequestID *string `json:"requestId"`
		Payload   *struct {
			Type    *string `json:"type"`
			Payload *struct {
				TeamID      *string `json:"team_id"`
				UserGroupID *string `json:"user_group_id"`
				ConvID      *string `json:"conv_id"`
				Actor       *string `json:"actor"`
			} `json:"payload"`
		} `json:"payload"`
	}
	invalid := errors.New("invalid background job")
	if json.Unmarshal(body, &raw) != nil || raw.ID == nil || raw.RequestID == nil || raw.Payload == nil || raw.Payload.Type == nil || raw.Payload.Payload == nil {
		return backgroundJob{}, invalid
	}
	p := raw.Payload.Payload
	if p.TeamID == nil || p.UserGroupID == nil {
		return backgroundJob{}, invalid
	}
	tag := *raw.Payload.Type
	if tag != "sync-user-group" && tag != "sync-user-group-and-channel" || tag == "sync-user-group-and-channel" && p.ConvID == nil {
		return backgroundJob{}, invalid
	}
	ids := []*string{raw.ID, p.TeamID, p.UserGroupID}
	if tag == "sync-user-group-and-channel" {
		ids = append(ids, p.ConvID)
	}
	if p.Actor != nil {
		ids = append(ids, p.Actor)
	}
	for _, id := range ids {
		uuid, err := parseUserID(*id)
		if err != nil {
			return backgroundJob{}, invalid
		}
		*id = uuid.String()
	}
	job := backgroundJob{ID: *raw.ID, RequestID: *raw.RequestID, Payload: backgroundJobPayload{Type: tag, Payload: groupSyncPayload{TeamID: *p.TeamID, UserGroupID: *p.UserGroupID, Actor: p.Actor}}}
	if tag == "sync-user-group-and-channel" {
		job.Payload.Payload.ConvID = *p.ConvID
	}
	return job, nil
}

// This boundary is wired only after both source business handlers exist.
type jobDispatcher interface {
	Dispatch(context.Context, backgroundJob) error
}

type jobMetrics struct {
	received, started, succeeded, failed, invalid, redelivered uint64
	count                                                      uint64
	sum                                                        float64
	buckets                                                    [11]uint64
}

var jobDurationBuckets = [...]float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

func (s *Service) jobMetric(label string, update func(*jobMetrics)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.jobs == nil {
		s.jobs = make(map[string]*jobMetrics)
	}
	m := s.jobs[label]
	if m == nil {
		m = &jobMetrics{}
		s.jobs[label] = m
	}
	update(m)
}

func (s *Service) handleJob(ctx context.Context, dispatcher jobDispatcher, delivery amqp.Delivery) error {
	job, err := decodeBackgroundJob(delivery.Body)
	if err != nil {
		s.jobMetric("invalid", func(m *jobMetrics) { m.invalid++ })
		if !wait(ctx, 200*time.Millisecond) {
			return ctx.Err() // Shutdown leaves the delivery unacknowledged.
		}
		return delivery.Reject(true)
	}
	label := job.Payload.Type
	s.jobMetric(label, func(m *jobMetrics) {
		m.received++
		if delivery.Redelivered {
			m.redelivered++
		}
	})
	s.mu.Lock()
	s.jobsBusy++
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.jobsBusy--; s.mu.Unlock() }()
	delay := 100 * time.Millisecond
	for attempt := 0; attempt < s.Config.BackgroundJobs.Attempts; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		s.jobMetric(label, func(m *jobMetrics) { m.started++ })
		start := time.Now()
		attemptCtx, cancel := context.WithTimeout(ctx, time.Duration(s.Config.BackgroundJobs.Timeout))
		err = dispatcher.Dispatch(attemptCtx, job)
		if attemptCtx.Err() != nil {
			err = attemptCtx.Err()
		}
		cancel()
		elapsed := time.Since(start).Seconds()
		s.jobMetric(label, func(m *jobMetrics) {
			m.count++
			m.sum += elapsed
			for i, upper := range jobDurationBuckets {
				if elapsed <= upper {
					m.buckets[i]++
				}
			}
		})
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err == nil {
			s.jobMetric(label, func(m *jobMetrics) { m.succeeded++ })
			return delivery.Ack(false)
		}
		if attempt+1 < s.Config.BackgroundJobs.Attempts {
			if !wait(ctx, time.Duration(rand.Int64N(int64(delay)+1))) {
				return ctx.Err()
			}
			// Avoid overflow without a tighter operational retry cap. A job's
			// configured attempt count and cancellation bound the entire loop.
			if delay <= time.Duration(1<<62-1) {
				delay *= 2
			} else {
				delay = time.Duration(1<<63 - 2)
			}
		}
	}
	s.jobMetric(label, func(m *jobMetrics) { m.failed++ })
	return delivery.Reject(false)
}

func ensureBackgroundJobsQueue(channel *amqp.Channel) error {
	_, err := channel.QueueDeclare(backgroundJobsQueue, true, false, false, false, amqp.Table{"x-queue-type": "quorum"})
	return err
}

func publishBackgroundJob(ctx context.Context, channel *amqp.Channel, job backgroundJob) error {
	if err := ensureBackgroundJobsQueue(channel); err != nil {
		return errors.New("background job queue declaration failed")
	}
	body, err := json.Marshal(job)
	if err != nil {
		return errors.New("background job serialization failed")
	}
	// Native BackgroundJobsPublisher uses newMsg without setting delivery mode
	// or publisher confirms. Preserve that property and record its inherited risk.
	if err = channel.PublishWithContext(ctx, "", backgroundJobsQueue, false, false, amqp.Publishing{ContentType: "application/json", MessageId: job.ID, CorrelationId: job.RequestID, Body: body}); err != nil {
		return errors.New("background job publication failed")
	}
	return nil
}

func newJobID() (string, error) {
	var id [16]byte
	if _, err := crand.Read(id[:]); err != nil {
		return "", errors.New("background job identifier generation failed")
	}
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:]), nil
}

func (s *Service) jobSession(ctx context.Context, channel *amqp.Channel, dispatcher jobDispatcher) error {
	if err := ensureBackgroundJobsQueue(channel); err != nil {
		return err
	}
	if err := channel.Qos(s.Config.BackgroundJobs.Concurrency, 0, false); err != nil {
		return err
	}
	deliveries, err := channel.Consume(backgroundJobsQueue, "background-job-consumer", false, false, false, false, nil)
	if err != nil {
		return err
	}
	s.mark(2, true)
	defer s.mark(2, false)
	workerCtx, stop := context.WithCancel(ctx)
	defer stop()
	closed := channel.NotifyClose(make(chan *amqp.Error, 1))
	failed := make(chan error, s.Config.BackgroundJobs.Concurrency)
	var wg sync.WaitGroup
	defer func() { stop(); _ = channel.Cancel("background-job-consumer", false); wg.Wait() }()
	for range s.Config.BackgroundJobs.Concurrency {
		wg.Go(func() {
			for {
				select {
				case <-workerCtx.Done():
					return
				case delivery, ok := <-deliveries:
					if !ok {
						failed <- errors.New("background job consumer closed")
						return
					}
					if err := s.handleJob(workerCtx, dispatcher, delivery); err != nil {
						failed <- err
						return
					}
				}
			}
		})
	}
	select {
	case <-ctx.Done():
		return nil
	case <-closed:
		return errors.New("background job channel closed")
	case <-failed:
		return errors.New("background job acknowledgement failed")
	}
}
