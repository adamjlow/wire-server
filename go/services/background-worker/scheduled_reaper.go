// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

//go:embed arbiter_sql/reaper/*
var scheduledReaperFiles embed.FS

func scheduledReaperSQL(name, queue string) (string, error) {
	if strings.ContainsAny(name, "/\\.") {
		return "", errors.New("invalid scheduled reaper operation")
	}
	body, err := scheduledReaperFiles.ReadFile("arbiter_sql/reaper/" + name + ".sql")
	if err != nil {
		return "", errors.New("unknown scheduled reaper operation")
	}
	if strings.Contains(string(body), "{{queue}}") {
		return renderScheduledSQL(string(body), queue)
	}
	return string(body), nil
}

func reaperQuery(ctx context.Context, tx pgx.Tx, name, queue string, args ...any) (pgx.Rows, error) {
	sql, err := scheduledReaperSQL(name, queue)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, postgresFailure(err, "scheduled reaper query failed")
	}
	return rows, nil
}

func reaperExec(ctx context.Context, tx pgx.Tx, name, queue string, args ...any) (int64, error) {
	sql, err := scheduledReaperSQL(name, queue)
	if err != nil {
		return 0, err
	}
	result, err := tx.Exec(ctx, sql, args...)
	if err != nil {
		return 0, postgresFailure(err, "scheduled reaper statement failed")
	}
	return result.RowsAffected(), nil
}

func reaperSavepoint(ctx context.Context, tx pgx.Tx, action func(pgx.Tx) (int64, error)) (int64, error) {
	child, err := tx.Begin(ctx)
	if err != nil {
		return 0, postgresFailure(err, "scheduled reaper savepoint failed")
	}
	defer func() { _ = child.Rollback(ctx) }()
	count, err := action(child)
	if err != nil {
		return 0, err
	}
	if err := child.Commit(ctx); err != nil {
		return 0, postgresFailure(err, "scheduled reaper savepoint commit failed")
	}
	return count, nil
}

func (s *postgresStore) runScheduledGate(ctx context.Context, task string, interval, limit time.Duration, action func(pgx.Tx) error) (ran bool, err error) {
	if interval <= 0 || limit <= 0 {
		return false, errors.New("invalid scheduled reaper configuration")
	}
	err = retryPostgres(ctx, s.Pool.Reset, func() error {
		ran = false
		conn, err := s.acquire(ctx)
		if err != nil {
			return err
		}
		defer conn.Release()
		sql, _ := scheduledReaperSQL("ensureGateRowSQL", "")
		if _, err := conn.Exec(ctx, sql, task); err != nil {
			return postgresFailure(err, "scheduled gate creation failed")
		}
		sql, _ = scheduledReaperSQL("checkGateSQL", "")
		var open bool
		err = conn.QueryRow(ctx, sql, task, interval.Seconds()).Scan(&open)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return postgresFailure(err, "scheduled gate lookup failed")
		}
		if err == nil && !open {
			return nil
		}
		tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
		if err != nil {
			return postgresFailure(err, "scheduled gate transaction failed")
		}
		defer func() { _ = tx.Rollback(ctx) }()
		sql, _ = scheduledReaperSQL("tryClaimGateSQL", "")
		var claim int64
		err = tx.QueryRow(ctx, sql, task, interval.Seconds()).Scan(&claim)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return postgresFailure(err, "scheduled gate claim failed")
		}
		millis := limit / time.Millisecond
		if limit%time.Millisecond != 0 {
			millis++
		}
		if _, err := tx.Exec(ctx, `SELECT set_config('statement_timeout',$1::text,true)`, strconv.FormatInt(int64(millis), 10)); err != nil {
			return postgresFailure(err, "scheduled reaper timeout configuration failed")
		}
		if err := action(tx); err != nil {
			return err
		}
		if _, err := reaperExec(ctx, tx, "bumpGateSQL", "", task); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return postgresFailure(err, "scheduled gate commit failed")
		}
		ran = true
		return nil
	})
	return
}

func reaperIDs(ctx context.Context, tx pgx.Tx, name, queue string, args ...any) ([]int64, error) {
	rows, err := reaperQuery(ctx, tx, name, queue, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) != nil {
			return nil, errors.New("invalid scheduled reaper identifier")
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, postgresFailure(err, "scheduled reaper identifier read failed")
	}
	return ids, nil
}

func reaperResumeParent(ctx context.Context, tx pgx.Tx, queue string, id int64) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text,$2::bigint))`, "arbiter."+queue, id); err != nil {
		return postgresFailure(err, "scheduled reaper parent lock failed")
	}
	_, err := reaperExec(ctx, tx, "tryWakeAncestorSQL", queue, id)
	return err
}

func reaperRefreshGroups(ctx context.Context, tx pgx.Tx, queue string) (int64, error) {
	return reaperSavepoint(ctx, tx, func(tx pgx.Tx) (int64, error) {
		rows, err := reaperQuery(ctx, tx, "lockGroupsSQL", queue)
		if err != nil {
			return 0, err
		}
		keys := []string{}
		for rows.Next() {
			var key string
			if rows.Scan(&key) != nil {
				rows.Close()
				return 0, errors.New("invalid scheduled group key")
			}
			keys = append(keys, key)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return 0, postgresFailure(err, "scheduled group lock read failed")
		}
		_, err = reaperExec(ctx, tx, "refreshGroupsSQL", queue, keys)
		return 0, err
	})
}

func reaperCancelled(ctx context.Context, tx pgx.Tx, queue string) (int64, error) {
	ids, err := reaperIDs(ctx, tx, "selectCancelledReapableJobsSQL", queue)
	if err != nil || len(ids) == 0 {
		return 0, err
	}
	return reaperSavepoint(ctx, tx, func(tx pgx.Tx) (int64, error) {
		rows, err := reaperQuery(ctx, tx, "deleteCancelledJobsSQL", queue, ids)
		if err != nil {
			return 0, err
		}
		var count int64
		parents := make(map[int64]bool)
		for rows.Next() {
			var parent *int64
			if rows.Scan(&parent) != nil {
				rows.Close()
				return 0, errors.New("invalid scheduled cancelled job parent")
			}
			count++
			if parent != nil {
				parents[*parent] = true
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return 0, postgresFailure(err, "scheduled cancelled parent read failed")
		}
		keys := make([]int64, 0, len(parents))
		for id := range parents {
			keys = append(keys, id)
		}
		sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
		for _, id := range keys {
			if err := reaperResumeParent(ctx, tx, queue, id); err != nil {
				return 0, err
			}
		}
		return count, nil
	})
}

type reaperChildResult struct {
	Source string
	ID     *int64
	Value  json.RawMessage
	Error  *string
	DLQID  *int64
}

// Native Either serialization and left-biased merge: DLQ errors > results >
// snapshot. A malformed snapshot is discarded as a whole, not partly retained.
func mergeReaperResults(rows []reaperChildResult) (map[string]json.RawMessage, error) {
	result := make(map[string]json.RawMessage)
	for _, row := range rows {
		if row.Source != "s" {
			continue
		}
		var snapshot map[string]json.RawMessage
		if json.Unmarshal(row.Value, &snapshot) != nil || snapshot == nil {
			result = make(map[string]json.RawMessage)
			continue
		}
		valid := true
		for id, value := range snapshot {
			if _, err := strconv.ParseInt(id, 10, 64); err != nil {
				valid = false
				break
			}
			var either map[string]json.RawMessage
			if json.Unmarshal(value, &either) != nil || len(either) != 1 {
				valid = false
				break
			}
			if left, ok := either["Left"]; ok {
				var text string
				if json.Unmarshal(left, &text) != nil || string(left) == "null" {
					valid = false
					break
				}
			} else if _, ok := either["Right"]; !ok {
				valid = false
				break
			}
		}
		if valid {
			result = snapshot
		} else {
			result = make(map[string]json.RawMessage)
		}
	}
	for _, tag := range []string{"r", "e"} {
		for _, row := range rows {
			switch row.Source {
			case "s":
				if row.Value == nil {
					return nil, errors.New("invalid scheduled rollup snapshot row")
				}
			case "r":
				if row.ID == nil || row.Value == nil {
					return nil, errors.New("invalid scheduled rollup result row")
				}
			case "e":
				if row.ID == nil || row.DLQID == nil {
					return nil, errors.New("invalid scheduled rollup error row")
				}
			default:
				return nil, errors.New("invalid scheduled rollup row tag")
			}
			if row.Source != tag {
				continue
			}
			value := row.Value
			constructor := "Right"
			if tag == "e" {
				constructor = "Left"
				text := ""
				if row.Error != nil {
					text = *row.Error
				}
				value, _ = json.Marshal(text)
			}
			result[strconv.FormatInt(*row.ID, 10)] = json.RawMessage(`{"` + constructor + `":` + string(value) + `}`)
		}
	}
	return result, nil
}

func reaperSnapshotRollups(ctx context.Context, tx pgx.Tx, queue string, root int64) error {
	ids, err := reaperIDs(ctx, tx, "descendantRollupIdsSQL", queue, root)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := snapshotScheduledRollup(ctx, tx, queue, id); err != nil {
			return err
		}
	}

	return nil
}

func reaperExhausted(ctx context.Context, tx pgx.Tx, queue string) (int64, error) {
	rows, err := reaperQuery(ctx, tx, "selectExhaustedJobsSQL", queue)
	if err != nil {
		return 0, err
	}
	type exhausted struct {
		ID       int64
		Attempts int32
		Parent   *int64
		Rollup   bool
	}
	var jobs []exhausted
	for rows.Next() {
		var job exhausted
		if rows.Scan(&job.ID, &job.Attempts, &job.Parent, &job.Rollup) != nil {
			rows.Close()
			return 0, errors.New("invalid scheduled exhausted job")
		}
		jobs = append(jobs, job)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, postgresFailure(err, "scheduled exhausted job read failed")
	}
	var count int64
	for _, job := range jobs {
		moved, err := reaperSavepoint(ctx, tx, func(tx pgx.Tx) (int64, error) {
			if job.Rollup {
				if err := reaperSnapshotRollups(ctx, tx, queue, job.ID); err != nil {
					return 0, err
				}
				ids, err := reaperIDs(ctx, tx, "cascadeChildrenToDLQSQL", queue, job.ID, "Parent moved to DLQ")
				if err != nil {
					return 0, err
				}
				if len(ids) != 1 {
					return 0, errors.New("invalid scheduled cascade DLQ count")
				}
			}
			ids, err := reaperIDs(ctx, tx, "moveToDLQSQL", queue, job.ID, job.Attempts, "max attempts exceeded (reaper sweep)")
			if err != nil {
				return 0, err
			}
			if len(ids) != 1 {
				return 0, errors.New("invalid scheduled DLQ count")
			}
			if ids[0] > 0 && job.Parent != nil {
				if err := reaperResumeParent(ctx, tx, queue, *job.Parent); err != nil {
					return 0, err
				}
			}
			return ids[0], nil
		})
		if err != nil {
			return 0, err
		}
		count += moved
	}
	return count, nil
}

type scheduledReaperResult struct {
	Task         string
	Ran          bool
	Count        int64
	FailedQueues []string
	Err          error
}

func (s *postgresStore) reapScheduledJobs(ctx context.Context, interval, limit time.Duration) []scheduledReaperResult {
	var results []scheduledReaperResult
	for _, task := range []string{"refresh-all-groups", "sweep-stale-workers", "sweep-exhausted-jobs", "sweep-cancelled-jobs", "purge-archives"} {
		result := scheduledReaperResult{Task: task}
		result.Ran, result.Err = s.runScheduledGate(ctx, task, interval, limit, func(tx pgx.Tx) error {
			result.Count, result.FailedQueues = 0, nil
			if task == "sweep-stale-workers" {
				count, err := reaperExec(ctx, tx, "deleteStaleWorkersSQL", "")
				result.Count = count
				return err
			}
			for _, queue := range []string{"meetings", "conversations"} {
				var count int64
				var err error
				switch task {
				case "refresh-all-groups":
					count, err = reaperRefreshGroups(ctx, tx, queue)
				case "sweep-exhausted-jobs":
					count, err = reaperExhausted(ctx, tx, queue)
				case "sweep-cancelled-jobs":
					count, err = reaperCancelled(ctx, tx, queue)
				case "purge-archives":
					count, err = reaperSavepoint(ctx, tx, func(tx pgx.Tx) (int64, error) { return reaperExec(ctx, tx, "purgeArchiveSQL", queue) })
				}
				if err != nil {
					result.FailedQueues = append(result.FailedQueues, queue)
				} else {
					result.Count += count
				}
			}
			return nil
		})
		results = append(results, result)
	}
	return results
}

func snapshotScheduledRollup(ctx context.Context, tx pgx.Tx, queue string, id int64) error {

	rows, err := reaperQuery(ctx, tx, "readChildResultsSQL", queue, id)
	if err != nil {
		return err
	}
	var values []reaperChildResult
	for rows.Next() {
		var row reaperChildResult
		if rows.Scan(&row.Source, &row.ID, &row.Value, &row.Error, &row.DLQID) != nil {
			rows.Close()
			return errors.New("invalid scheduled rollup data")
		}
		values = append(values, row)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return postgresFailure(err, "scheduled rollup data read failed")
	}
	merged, err := mergeReaperResults(values)
	if err != nil {
		return err
	}
	if len(merged) > 0 {
		blob, err := json.Marshal(merged)
		if err != nil {
			return errors.New("scheduled rollup encoding failed")
		}
		if _, err := reaperExec(ctx, tx, "persistParentStateSQL", queue, blob, id); err != nil {
			return err
		}
	}
	return nil
}
