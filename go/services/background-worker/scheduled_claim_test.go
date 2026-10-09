// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"testing"
	"time"
)

func TestScheduledStoreRejectsForeignQueueBeforeDatabaseAccess(t *testing.T) {
	// No pool: rejection must happen before any database acquisition. Queue
	// names are the only SQL template substitution; every value is bound.
	store := &postgresStore{}
	for _, queue := range []string{"", "conversations; DROP TABLE user", "meetings\"", "other"} {
		if _, err := store.ClaimScheduledJobs(t.Context(), queue, jobUUID, time.Minute, 1); err == nil {
			t.Fatal("foreign scheduled claim queue accepted")
		}
		if _, err := store.HeartbeatScheduledJob(t.Context(), queue, 1, 1, time.Minute); err == nil {
			t.Fatal("foreign heartbeat queue accepted")
		}
		if _, err := store.RetryScheduledJob(t.Context(), queue, 1, 1, 1); err == nil {
			t.Fatal("foreign retry queue accepted")
		}
	}
	if _, err := store.ClaimScheduledJobs(t.Context(), "meetings", "invalid-uuid", time.Minute, 1); err == nil {
		t.Fatal("invalid worker identity accepted")
	}
	if _, err := store.ClaimScheduledJobs(t.Context(), "meetings", jobUUID, time.Minute, 1001); err == nil {
		t.Fatal("unbounded claim budget accepted")
	}
}
