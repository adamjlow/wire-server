// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

var workerNames = []string{"backend-notification-pusher", "dead-user-notification-watcher", "background-job-consumer"}

type Service struct {
	Config        Config
	Galley        GalleyConfig
	mu            sync.Mutex
	working       [3]bool
	notifications map[string]*notificationMetrics
	jobs          map[string]*jobMetrics
	jobsBusy      int
}
type notificationMetrics struct {
	pushed, dropped, errors uint64
	stuck                   bool
}

func New(c Config, g GalleyConfig) *Service {
	return &Service{Config: c, Galley: g, notifications: make(map[string]*notificationMetrics)}
}
func (s *Service) mark(index int, working bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.working[index] = working
}
func (s *Service) metric(domain string, update func(*notificationMetrics)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.notifications[domain]
	if m == nil {
		m = &notificationMetrics{}
		s.notifications[domain] = m
	}
	update(m)
}

func (s *Service) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/i/status" && r.URL.Path != "/i/status/workers" && r.URL.Path != "/i/metrics" {
			http.NotFound(w, r)
			return
		}
		if r.Method != "GET" {
			w.Header().Set("Allow", "GET")
			w.WriteHeader(405)
			return
		}
		if r.URL.Path == "/i/status" {
			w.Header().Set("Content-Type", "text/plain;charset=utf-8")
			w.WriteHeader(200)
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if r.URL.Path == "/i/status/workers" {
			missing := []string{}
			for i, name := range workerNames {
				if !s.working[i] {
					missing = append(missing, name)
				}
			}
			w.Header().Set("Content-Type", "text/plain;charset=utf-8")
			if len(missing) > 0 {
				w.WriteHeader(500)
				_, _ = fmt.Fprint(w, "These workers are not working: "+strings.Join(missing, ", "))
			}
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		for i, name := range workerNames {
			n := 0
			if s.working[i] {
				n = 1
			}
			_, _ = fmt.Fprintf(w, "wire_background_worker_running_workers{worker=%q} %d\n", name, n)
		}
		for domain, m := range s.notifications {
			n := 0
			if m.stuck {
				n = 1
			}
			_, _ = fmt.Fprintf(w, "wire_backend_notifications_pushed{targetDomain=%q} %d\nwire_backend_notifications_dropped_unsupported_version{targetDomain=%q} %d\nwire_backend_notifications_errors{targetDomain=%q} %d\nwire_backend_notifications_stuck_queues{targetDomain=%q} %d\n", domain, m.pushed, domain, m.dropped, domain, m.errors, domain, n)
		}
		_, _ = fmt.Fprintf(w, "wire_background_jobs_workers_busy %d\nwire_background_jobs_concurrency_configured %d\n", s.jobsBusy, s.Config.BackgroundJobs.Concurrency)
		for _, label := range []string{"sync-user-group", "sync-user-group-and-channel", "invalid"} {
			m := s.jobs[label]
			if m == nil {
				continue
			}
			for _, counter := range []struct {
				name  string
				value uint64
			}{{"received", m.received}, {"started", m.started}, {"succeeded", m.succeeded}, {"failed", m.failed}, {"invalid", m.invalid}, {"redelivered", m.redelivered}} {
				_, _ = fmt.Fprintf(w, "wire_background_jobs_%s_total{job_type=%q} %d\n", counter.name, label, counter.value)
			}
			if m.count > 0 {
				for i, upper := range jobDurationBuckets {
					_, _ = fmt.Fprintf(w, "wire_background_jobs_duration_seconds_bucket{job_type=%q,le=%q} %d\n", label, fmt.Sprint(upper), m.buckets[i])
				}
				_, _ = fmt.Fprintf(w, "wire_background_jobs_duration_seconds_bucket{job_type=%q,le=\"+Inf\"} %d\nwire_background_jobs_duration_seconds_sum{job_type=%q} %g\nwire_background_jobs_duration_seconds_count{job_type=%q} %d\n", label, m.count, label, m.sum, label, m.count)
			}
		}
	})
}

func (s *Service) Run(ctx context.Context) error {
	// Early construction slice: migration/scheduled/group-sync code is unfinished.
	// Never silently accept enabled data migrations before those paths exist.
	if s.Config.MigrateConversations || s.Config.MigrateCodes || s.Config.MigrateTeamFeatures || s.Config.MigrateDomains || s.Config.MigrateUsers || s.Config.MigrateBlockList || s.Config.MigratePasswordReset {
		return errors.New("candidate construction incomplete: migrations are not implemented")
	}
	listener, err := net.Listen("tcp", s.Config.HTTP.Address())
	if err != nil {
		return errors.New("health listener could not start")
	}
	server := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 75 * time.Second, MaxHeaderBytes: 1 << 20}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	wg.Go(func() { s.runPusher(runCtx) })
	wg.Go(func() { s.runDeadWatcher(runCtx) })
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	select {
	case <-ctx.Done():
	case err = <-done:
	}
	cancel()
	graceCtx := context.Background()
	stop := func() {}
	if s.Config.Jobs.Grace != nil {
		graceCtx, stop = context.WithTimeout(graceCtx, time.Duration(*s.Config.Jobs.Grace))
	}
	defer stop()
	_ = server.Shutdown(graceCtx)
	finished := make(chan struct{})
	go func() { wg.Wait(); close(finished) }()
	select {
	case <-finished:
	case <-graceCtx.Done():
		return errors.New("worker shutdown exceeded configured grace")
	}
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return errors.New("health server failed")
	}
	return nil
}
