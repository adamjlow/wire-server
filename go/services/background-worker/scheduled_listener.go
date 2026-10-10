// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
)

// One dedicated connection serves both pools, outside the handler/acquisition
// pool. Subscriptions are recreated after disconnect; readiness is observability
// only and never blocks dispatcher startup. Native reconnect1s doubles to30s,
// reset after a connection survives30s. Polling remains available during gaps.
func runScheduledListener(ctx context.Context, config *pgx.ConnConfig, pools []*scheduledPool, ready func(bool), report func(error)) {
	if config == nil {
		if ready != nil {
			ready(true)
		}
		return
	}
	publishedReady := false
	if ready != nil {
		ready(false)
	}
	channels := []string{}
	for _, p := range pools {
		channels = append(channels, p.Queue+"_created", "arbiter_pause_arbiter_"+p.Queue, "arbiter_cancel_arbiter_"+p.Queue)
		if p.Queue == "meetings" {
			channels = append(channels, "arbiter_cron_run_arbiter")
		}
	}
	slices.Sort(channels)
	channels = slices.Compact(channels)
	delay := time.Second
	for ctx.Err() == nil {

		connectedAt := time.Time{}
		conn, err := pgx.ConnectConfig(ctx, config.Copy())
		if err == nil {
			connectedAt = time.Now()
			for _, channel := range channels {
				_, err = conn.Exec(ctx, "LISTEN "+pgx.Identifier{channel}.Sanitize())
				if err != nil {
					break
				}
			}
			if err == nil {
				if !publishedReady && ready != nil {
					ready(true)
					publishedReady = true
				}
				for ctx.Err() == nil {
					notification, waitErr := conn.WaitForNotification(ctx)
					if waitErr != nil {
						err = waitErr
						break
					}
					for _, p := range pools {
						p.Notify(notification.Channel, notification.Payload)
					}
				}
			}
			_ = conn.Close(ctx)
		}
		if ctx.Err() != nil {
			break
		}
		if report != nil {
			report(errors.New("scheduled listener connection failed"))
		}
		stable := !connectedAt.IsZero() && time.Since(connectedAt) >= 30*time.Second
		if !wait(ctx, delay) {
			break
		}
		if stable {
			delay = time.Second
		} else {
			delay = min(30*time.Second, delay*2)
		}
	}
}
