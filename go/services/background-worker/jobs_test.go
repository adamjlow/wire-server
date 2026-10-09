// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const jobUUID = "11111111-1111-1111-1111-111111111111"

func sourceGroupJob() backgroundJob {
	return backgroundJob{ID: jobUUID, RequestID: "synthetic-request", Payload: backgroundJobPayload{Type: "sync-user-group", Payload: groupSyncPayload{TeamID: jobUUID, UserGroupID: jobUUID}}}
}

type observedDispatcher struct {
	calls int
	run   func(context.Context, backgroundJob) error
}

func (d *observedDispatcher) Dispatch(ctx context.Context, job backgroundJob) error {
	d.calls++
	return d.run(ctx, job)
}

func jobService() *Service {
	c := Config{}
	c.BackgroundJobs.Concurrency = 2
	c.BackgroundJobs.Attempts = 2
	c.BackgroundJobs.Timeout = Duration(10 * time.Millisecond)
	return New(c, GalleyConfig{})
}

func TestBackgroundJobSourceSchemaAndInvalidPayloads(t *testing.T) {
	job := sourceGroupJob()
	for _, tag := range []string{"sync-user-group", "sync-user-group-and-channel"} {
		job.Payload.Type = tag
		job.Payload.Payload.ConvID = jobUUID
		job.Payload.Payload.Actor = ptr(jobUUID)
		got, err := decodeBackgroundJob(encode(t, job))
		if err != nil || got.Payload.Type != tag || got.RequestID != job.RequestID || got.Payload.Payload.Actor == nil || *got.Payload.Payload.Actor != jobUUID {
			t.Fatalf("source job changed: %+v %v", got, err)
		}
	}
	for _, body := range []string{`{}`, `null`, `{"id":null}`, string(encode(t, job)) + "{}", strings.Replace(string(encode(t, job)), jobUUID, "';DROP TABLE conversation;--", 1), strings.Replace(string(encode(t, job)), "sync-user-group-and-channel", "unknown", 1), strings.Replace(string(encode(t, job)), `"requestId":"synthetic-request"`, `"requestId":null`, 1), strings.Replace(string(encode(t, job)), `"conv_id":"`+jobUUID+`"`, `"conv_id":null`, 1)} {
		if _, err := decodeBackgroundJob([]byte(body)); err == nil {
			t.Fatalf("invalid job accepted: %s", body)
		}
	}
	// RequestId is source text without a nonempty constraint, actor is optional,
	// and unrelated fields are accepted by the source decoder.
	job = sourceGroupJob()
	job.RequestID = ""
	body := strings.TrimSuffix(string(encode(t, job)), "}") + `,"extra":true}`
	if _, err := decodeBackgroundJob([]byte(body)); err != nil {
		t.Fatal("valid source optional/extra fields rejected")
	}
}

func TestBackgroundJobsRetrySuccessExhaustionAndTimeout(t *testing.T) {
	for _, mode := range []string{"retry-success", "exhaustion", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			s := jobService()
			acks := &acknowledgements{}
			d := &observedDispatcher{}
			d.run = func(ctx context.Context, _ backgroundJob) error {
				if acks.acks != 0 || len(acks.rejects) != 0 {
					t.Fatal("job settled before dispatch finished")
				}
				if mode == "timeout" {
					<-ctx.Done()
					return ctx.Err()
				}
				if mode == "retry-success" && d.calls == 2 {
					return nil
				}
				return errors.New("synthetic job failure")
			}
			if err := s.handleJob(t.Context(), d, amqp.Delivery{Body: encode(t, sourceGroupJob()), DeliveryTag: 1, Acknowledger: acks, Redelivered: true}); err != nil {
				t.Fatal(err)
			}
			m := s.jobs["sync-user-group"]
			if d.calls != 2 || m.received != 1 || m.started != 2 || m.redelivered != 1 || m.count != 2 || s.jobsBusy != 0 {
				t.Fatal("source attempt/concurrency accounting changed")
			}
			if mode == "retry-success" {
				if acks.acks != 1 || len(acks.rejects) != 0 || m.succeeded != 1 || m.failed != 0 {
					t.Fatal("successful retried job was not ACKed")
				}
			} else if acks.acks != 0 || len(acks.rejects) != 1 || acks.rejects[0] || m.failed != 1 {
				t.Fatal("exhausted job was not rejected without requeue")
			}
			response := httptest.NewRecorder()
			s.Handler().ServeHTTP(response, httptest.NewRequest("GET", "/i/metrics", nil))
			if !strings.Contains(response.Body.String(), `wire_background_jobs_duration_seconds_count{job_type="sync-user-group"} 2`) {
				t.Fatal("missing source duration metric")
			}
		})
	}
}

func TestBackgroundJobMalformedRequeuesAndCancellationPreservesDelivery(t *testing.T) {
	s := jobService()
	acks := &acknowledgements{}
	d := &observedDispatcher{run: func(context.Context, backgroundJob) error { t.Fatal("invalid job dispatched"); return nil }}
	start := time.Now()
	if err := s.handleJob(t.Context(), d, amqp.Delivery{Body: []byte("{}"), Acknowledger: acks}); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) < 200*time.Millisecond || len(acks.rejects) != 1 || !acks.rejects[0] || d.calls != 0 || s.jobs["invalid"].invalid != 1 {
		t.Fatal("source poison-job delay/requeue contract changed")
	}
	acks = &acknowledgements{}
	ctx, cancel := context.WithCancel(t.Context())
	d.run = func(context.Context, backgroundJob) error { cancel(); return nil }
	if err := s.handleJob(ctx, d, amqp.Delivery{Body: encode(t, sourceGroupJob()), Acknowledger: acks}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled job was treated as completed")
	}
	if acks.acks != 0 || len(acks.rejects) != 0 || s.jobsBusy != 0 {
		t.Fatal("cancelled job lost its unacknowledged delivery")
	}
}

func TestBackgroundJobIdentifiersAreCanonicalRandomV4(t *testing.T) {
	first, err := newJobID()
	second, err2 := newJobID()
	if err != nil || err2 != nil || first == second || len(first) != 36 || first[14] != '4' || !strings.ContainsAny(first[19:20], "89ab") {
		t.Fatal("invalid or reused background job identifier")
	}
	if _, err := parseUserID(first); err != nil {
		t.Fatal(err)
	}
}
