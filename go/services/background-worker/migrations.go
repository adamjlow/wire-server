// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"errors"
	"math"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"
)

type migrationOptions struct {
	PageSize    int      `yaml:"pageSize"`
	Parallelism int      `yaml:"parallelism"`
	Timeout     Duration `yaml:"timeout"`
}

func (o migrationOptions) valid() bool {
	return o.PageSize > 0 && o.PageSize <= math.MaxInt32 && o.Parallelism > 0 && o.Parallelism <= math.MaxInt32 && o.Timeout > 0
}

type migrationRow map[string]any
type migrationPage func(context.Context, []byte, int) ([]migrationRow, []byte, error)
type migrationWrite func(context.Context, migrationRow) (bool, error)

type migrationObservation struct {
	count   uint64
	sum     float64
	buckets [len(jobDurationBuckets)]uint64
}

type migrationMetrics struct {
	migrated, finished, failed uint64
	observations               map[string]*migrationObservation
}

func (s *Service) migrationMetric(name string, update func(*migrationMetrics)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.migrations == nil {
		s.migrations = make(map[string]*migrationMetrics)
	}
	m := s.migrations[name]
	if m == nil {
		m = &migrationMetrics{observations: make(map[string]*migrationObservation)}
		s.migrations[name] = m
	}
	update(m)
}

func (s *Service) observeMigration(name, outcome string, elapsed time.Duration) {
	s.migrationMetric(name, func(m *migrationMetrics) {
		o := m.observations[outcome]
		if o == nil {
			o = &migrationObservation{}
			m.observations[outcome] = o
		}
		seconds := elapsed.Seconds()
		o.count++
		o.sum += seconds
		for i, upper := range jobDurationBuckets {
			if seconds <= upper {
				o.buckets[i]++
			}
		}
	})
}

type migrationPhase struct {
	page  migrationPage
	write migrationWrite
	timed bool
	// Only user and conversation migrations use the native per-page pool.
	parallel bool
}

func (s *Service) migrateRows(ctx context.Context, name string, page migrationPage, write migrationWrite) error {
	return s.migratePhases(ctx, name, []migrationPhase{{page: page, write: write, timed: true}})
}

func (s *Service) migratePhases(ctx context.Context, name string, phases []migrationPhase) (err error) {
	defer func() {
		if err != nil {
			s.migrationMetric(name, func(m *migrationMetrics) { m.failed++ })
		}
	}()
	opts := s.Config.MigrationOptions
	if !opts.valid() {
		return errors.New("invalid migration options")
	}
	for {
		rowErrors := 0
		for _, phase := range phases {
			var state []byte
			for {
				if err := ctx.Err(); err != nil {
					return err
				}
				rows, next, err := phase.page(ctx, state, opts.PageSize)
				if err != nil {
					return err
				}
				caught, err := s.migratePage(ctx, name, phase, rows, opts)
				if err != nil {
					return err
				}
				rowErrors += caught
				if len(next) == 0 {
					break
				}
				state = next
			}
		}
		if rowErrors == 0 {
			s.migrationMetric(name, func(m *migrationMetrics) { m.finished++ })
			return nil
		}
		// Native rescans every phase after any caught PG/lock errors. Successful
		// copies are safe to upsert again; neither phase alone means finished.
	}
}

// Finish the entire current page before fetching another. Fatal errors cancel
// sibling operations and wait for their cleanup; caught PG/lock errors allow
// the rest of the page to finish before the full scan is repeated.
func (s *Service) migratePage(ctx context.Context, name string, phase migrationPhase, rows []migrationRow, opts migrationOptions) (int, error) {
	if !phase.parallel {
		caught := 0
		for _, row := range rows {
			retry, err := s.migrateRow(ctx, name, phase, row, opts)
			if err != nil {
				return caught, err
			}
			if retry {
				caught++
			}
		}
		return caught, nil
	}
	workers := min(opts.Parallelism, len(rows))
	group, attempt := errgroup.WithContext(ctx)
	var next, caught atomic.Int64
	for range workers {
		group.Go(func() error {
			for {
				if err := attempt.Err(); err != nil {
					return err
				}
				i := next.Add(1) - 1
				if i >= int64(len(rows)) {
					return nil
				}
				retry, err := s.migrateRow(attempt, name, phase, rows[i], opts)
				if err != nil {
					return err
				}
				if retry {
					caught.Add(1)
				}
			}
		})
	}
	err := group.Wait()
	return int(caught.Load()), err
}

func (s *Service) migrateRow(ctx context.Context, name string, phase migrationPhase, row migrationRow, opts migrationOptions) (caught bool, err error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	start := time.Now()
	attempt, cancel := ctx, func() {}
	if phase.timed {
		attempt, cancel = context.WithTimeout(ctx, time.Duration(opts.Timeout))
	}
	counted, err := phase.write(attempt, row)
	attemptErr := attempt.Err()
	cancel()
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if phase.timed && errors.Is(attemptErr, context.DeadlineExceeded) {
		s.observeMigration(name, "timeout", time.Since(start))
		return false, errors.New("migration row timed out")
	}
	if err != nil {
		if phase.timed {
			s.observeMigration(name, "error", time.Since(start))
		}
		var usage *postgresOperationError
		var lockTimeout *migrationLockTimeout
		if errors.As(err, &usage) || errors.As(err, &lockTimeout) {
			return true, nil
		}
		return false, err
	}
	if counted {
		if phase.timed {
			s.observeMigration(name, "success", time.Since(start))
		}
		s.migrationMetric(name, func(m *migrationMetrics) { m.migrated++ })
	}
	return false, nil
}
