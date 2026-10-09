// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"fmt"
	"io"
)

// Caller holds Service.mu, like the other worker metric renderers.
func (s *Service) writeMigrationMetrics(w io.Writer) {
	for name, m := range s.migrations {
		fmt.Fprintf(w, "wire_%s_migrated_to_pg %d.0\nwire_%s_migration_finished %d.0\nwire_%s_migration_failed %d.0\n", name, m.migrated, name, m.finished, name, m.failed)
		for outcome, o := range m.observations {
			for i, upper := range jobDurationBuckets {
				fmt.Fprintf(w, "wire_%s_migration_duration_seconds_bucket{outcome=%q,le=%q} %d\n", name, outcome, fmt.Sprint(upper), o.buckets[i])
			}
			fmt.Fprintf(w, "wire_%s_migration_duration_seconds_bucket{outcome=%q,le=\"+Inf\"} %d\nwire_%s_migration_duration_seconds_sum{outcome=%q} %g\nwire_%s_migration_duration_seconds_count{outcome=%q} %d\n", name, outcome, o.count, name, outcome, o.sum, name, outcome, o.count)
		}
	}
}
