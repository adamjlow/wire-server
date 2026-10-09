// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"errors"
	"math"
	"math/big"
	"math/rand/v2"
	"time"
)

func scheduledDoublePico(value float64) *big.Int {
	r := new(big.Rat).SetFloat64(value)
	return new(big.Int).Div(new(big.Int).Mul(r.Num(), big.NewInt(1000000000000)), r.Denom())
}

// Native base^attempts uses integral exponentiation; realToFrac Double=>Pico
// precedes cap/jitter, and Operations.updateJobForRetry ceilings whole seconds.
func scheduledRetrySeconds(base float64, cap time.Duration, jitter string, attempts int32, draw func(float64, float64) float64) (int64, error) {
	if base <= 0 || math.IsNaN(base) || math.IsInf(base, 0) || cap <= 0 || attempts < 0 {
		return 0, errors.New("invalid scheduled backoff")
	}
	capPico := new(big.Int).Mul(big.NewInt(int64(cap)), big.NewInt(1000))
	power, factor := 1.0, base
	for n := attempts; n > 0; n /= 2 {
		if n%2 == 1 {
			power *= factor
		}
		if n > 1 {
			factor *= factor
		}
	}
	delay := new(big.Int).Set(capPico)
	if !math.IsInf(power, 1) {
		computed := scheduledDoublePico(power)
		if computed.Cmp(delay) < 0 {
			delay = computed
		}
	}
	if draw == nil {
		draw = func(low, high float64) float64 { return low + rand.Float64()*(high-low) }
	}
	if jitter != "none" {
		half := new(big.Int)
		if jitter == "equal" {
			half.Div(delay, big.NewInt(2))
		} else if jitter != "full" {
			return 0, errors.New("invalid scheduled jitter")
		}
		width := new(big.Int).Set(delay)
		if jitter == "equal" {
			width.Set(half)
		}
		widthFloat, _ := new(big.Rat).SetFrac(width, big.NewInt(1000000000000)).Float64()
		sample := draw(0, widthFloat)
		if math.IsNaN(sample) || math.IsInf(sample, 0) || sample < 0 || sample > widthFloat {
			return 0, errors.New("invalid scheduled jitter sample")
		}
		delay.Add(half, scheduledDoublePico(sample))
	}
	seconds, remainder := new(big.Int), new(big.Int)
	seconds.QuoRem(delay, big.NewInt(1000000000000), remainder)
	if remainder.Sign() > 0 {
		seconds.Add(seconds, big.NewInt(1))
	}
	return seconds.Int64(), nil
}
