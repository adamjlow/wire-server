// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"container/list"
	"errors"
	"math"
	"net/netip"
	"sync"
	"time"
)

type tokenBucketConfig struct {
	Burst       uint64 `yaml:"burst"`
	InverseRate uint64 `yaml:"inverseRate"`
}

type passwordRateConfig struct {
	IP         tokenBucketConfig `yaml:"ipAddrLimit"`
	User       tokenBucketConfig `yaml:"userLimit"`
	Internal   tokenBucketConfig `yaml:"internalLimit"`
	IPv4Bits   int               `yaml:"ipv4CidrBlock"`
	IPv6Bits   int               `yaml:"ipv6CidrBlock"`
	MaxKeys    int               `yaml:"maxRateLimitedKeys"`
	Exceptions []string          `yaml:"ipAddressExceptions"`
}

type passwordRateKey struct {
	kind string
	id   string
	ip   netip.Addr
}

type rateBucket struct {
	key              passwordRateKey
	level, timestamp uint64
}

type passwordRateLimiter struct {
	mu         sync.Mutex
	config     passwordRateConfig
	exceptions []netip.Prefix
	buckets    map[passwordRateKey]*list.Element
	lru        list.List
	now        func() time.Time
}

func newPasswordRateLimiter(config passwordRateConfig) (*passwordRateLimiter, error) {
	if config.IPv4Bits < 0 || config.IPv4Bits > 32 || config.IPv6Bits < 0 || config.IPv6Bits > 128 || config.MaxKeys < 0 {
		return nil, errors.New("invalid password rate limit configuration")
	}
	l := &passwordRateLimiter{config: config, buckets: make(map[passwordRateKey]*list.Element), now: time.Now}
	for _, value := range config.Exceptions {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			ip, parseErr := netip.ParseAddr(value)
			if parseErr != nil {
				return nil, errors.New("invalid password rate limit exception")
			}
			prefix = netip.PrefixFrom(ip, ip.BitLen())
		}
		l.exceptions = append(l.exceptions, prefix.Masked())
	}
	return l, nil
}

func saturatedSubtract(x, y uint64) uint64 {
	if x > y {
		return x - y
	}
	return 0
}

// Same lazy-bucket update as Control.Concurrent.TokenBucket.tryAlloc1:
// rejected attempts retain the decayed level, never add another token.
func allocatePasswordToken(bucket *rateBucket, config tokenBucketConfig, now uint64) uint64 {
	if config.InverseRate == 0 {
		return 0
	}
	elapsed := saturatedSubtract(now, bucket.timestamp)
	decay, remainder := elapsed/config.InverseRate, elapsed%config.InverseRate
	bucket.level = saturatedSubtract(bucket.level, decay)
	bucket.timestamp = saturatedSubtract(now, remainder)
	level := bucket.level
	if level != math.MaxUint64 {
		level++
	}
	if level > config.Burst {
		return config.InverseRate - remainder
	}
	bucket.level = level
	return 0
}

func (l *passwordRateLimiter) check(key passwordRateKey) uint64 {
	if key.kind == "ip" {
		for _, prefix := range l.exceptions {
			if prefix.Contains(key.ip) {
				return 0
			}
		}
		bits := l.config.IPv6Bits
		if key.ip.Is4() {
			bits = l.config.IPv4Bits
		}
		key.ip = netip.PrefixFrom(key.ip, bits).Masked().Addr()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := uint64(max(0, l.now().UnixMicro()))
	var bucket *rateBucket
	if entry := l.buckets[key]; entry != nil {
		l.lru.MoveToFront(entry)
		bucket = entry.Value.(*rateBucket)
	} else {
		bucket = &rateBucket{key: key, timestamp: now}
		if l.config.MaxKeys > 0 {
			l.buckets[key] = l.lru.PushFront(bucket)
			if l.lru.Len() > l.config.MaxKeys {
				old := l.lru.Back()
				delete(l.buckets, old.Value.(*rateBucket).key)
				l.lru.Remove(old)
			}
		}
	}
	config := l.config.User
	switch key.kind {
	case "ip":
		config = l.config.IP
	case "internal":
		config = l.config.Internal
	}
	return allocatePasswordToken(bucket, config, now)
}

type passwordRateExceeded struct{ micros uint64 }

func (passwordRateExceeded) Error() string { return "password operation rate limited" }
