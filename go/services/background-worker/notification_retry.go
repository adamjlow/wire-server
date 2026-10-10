// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"math"
	"math/rand/v2"
	"time"
)

// Match Control.Retry0.9.3.1 fullJitterBackoff: saturated exponential Int
// microseconds, half plus an inclusive uniform draw. capDelay is applied AFTER
// this draw, not before. No new Go dependency or common retry framework.
func nativeNotificationDelay(base int64, iteration uint64, draw func(int64) int64) int64 {
	exponential := int64(math.MaxInt64)
	if iteration < 63 && base <= math.MaxInt64>>iteration {
		exponential = base << iteration
	}
	half := exponential / 2
	if draw == nil {
		draw = rand.Int64N
	}
	return half + draw(half+1)
}

// Native getRemoteDomains retries the entire traversal (starting at page1)
// with10ms base and60s accumulated WAIT budget. HTTP time is not charged to
// that budget. Cancellation skips policy consultation, like async exceptions.
func retryQueueDiscovery(ctx context.Context, action func() ([]string, error), draw func(int64) int64, pause func(context.Context, time.Duration) bool) ([]string, error) {
	cumulative := int64(0)
	for iteration := uint64(0); ; iteration++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		domains, err := action()
		if err == nil {
			return domains, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		delay := nativeNotificationDelay(10000, iteration, draw)
		if delay > 60000000-cumulative {
			return nil, err
		}
		if !pause(ctx, time.Duration(delay)*time.Microsecond) {
			return nil, ctx.Err()
		}
		cumulative += delay
	}
}
