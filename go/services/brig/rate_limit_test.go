// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"math"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPasswordBucketNativeDecayAndRejectedAttempts(t *testing.T) {
	b := rateBucket{timestamp: 1000}
	c := tokenBucketConfig{Burst: 2, InverseRate: 100}
	for _, step := range []struct{ now, wait, level, timestamp uint64 }{
		{1000, 0, 1, 1000}, {1000, 0, 2, 1000}, {1050, 50, 2, 1000},
		{1099, 1, 2, 1000}, {1100, 0, 2, 1100}, {900, 100, 2, 900},
		{2000, 0, 1, 2000},
	} {
		if wait := allocatePasswordToken(&b, c, step.now); wait != step.wait || b.level != step.level || b.timestamp != step.timestamp {
			t.Fatalf("bucket transition at %d: wait=%d level=%d time=%d", step.now, wait, b.level, b.timestamp)
		}
	}
	if allocatePasswordToken(&b, tokenBucketConfig{InverseRate: 0}, 0) != 0 {
		t.Fatal("zero inverse rate must bypass")
	}
	b.level = math.MaxUint64
	if allocatePasswordToken(&b, tokenBucketConfig{Burst: math.MaxUint64, InverseRate: 100}, 2000) != 0 || b.level != math.MaxUint64 {
		t.Fatal("native saturation changed")
	}
}

func TestPasswordRateMaskExceptionsNamespacesAndLRU(t *testing.T) {
	c := passwordRateConfig{IP: tokenBucketConfig{1, 100}, User: tokenBucketConfig{1, 100}, Internal: tokenBucketConfig{1, 100}, IPv4Bits: 24, IPv6Bits: 64, MaxKeys: 2, Exceptions: []string{"127.0.0.1/8"}}
	l, err := newPasswordRateLimiter(c)
	if err != nil {
		t.Fatal(err)
	}
	l.now = func() time.Time { return time.UnixMicro(1000) }
	ip := func(s string) passwordRateKey { return passwordRateKey{kind: "ip", ip: netip.MustParseAddr(s)} }
	if l.check(ip("192.0.2.1")) != 0 || l.check(ip("192.0.2.200")) != 100 {
		t.Fatal("IPv4 subnet quota not shared")
	}
	if l.check(ip("127.1.2.3")) != 0 || len(l.buckets) != 1 {
		t.Fatal("exceptions must bypass storage")
	}
	if l.check(ip("2001:db8::1")) != 0 || l.check(ip("2001:db8::abcd")) != 100 {
		t.Fatal("IPv6 subnet quota not shared")
	}
	// Touch IPv4, then a third key evicts IPv6. Provider/user namespaces differ.
	if l.check(ip("192.0.2.1")) != 100 {
		t.Fatal("touch lost bucket")
	}
	key := passwordRateKey{kind: "user", id: "synthetic"}
	if l.check(key) != 0 || l.check(key) != 100 || len(l.buckets) != 2 {
		t.Fatal("bounded LRU quota incorrect")
	}
	if l.check(ip("2001:db8::1")) != 0 {
		t.Fatal("evicted bucket should restart")
	}
	if l.check(passwordRateKey{kind: "provider", id: "synthetic"}) != 0 {
		t.Fatal("provider and user keys must remain distinct")
	}
}

func TestPasswordRateConcurrentBurst(t *testing.T) {
	l, err := newPasswordRateLimiter(passwordRateConfig{User: tokenBucketConfig{7, 1000}, MaxKeys: 10})
	if err != nil {
		t.Fatal(err)
	}
	l.now = func() time.Time { return time.UnixMicro(1000) }
	var accepted atomic.Int32
	var workers sync.WaitGroup
	for range 64 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if l.check(passwordRateKey{kind: "user", id: "synthetic"}) == 0 {
				accepted.Add(1)
			}
		}()
	}
	workers.Wait()
	if accepted.Load() != 7 {
		t.Fatalf("concurrent quota accepted %d instead of7", accepted.Load())
	}
}
