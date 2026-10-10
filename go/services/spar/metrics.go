// SPDX-License-Identifier: AGPL-3.0-or-later
package spar

import (
	"context"
	"fmt"
	"math"
	"net"
	"net/http"
	"runtime"
	runtimemetrics "runtime/metrics"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var latencyBounds = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, math.Inf(1)}
var latencyNames = []string{"0.005", "0.01", "0.025", "0.05", "0.1", "0.25", "0.5", "1.0", "2.5", "5.0", "10.0", "+Inf"}

// Capture names are from Wire.API.Routes.{Public,Internal}.Spar and hscim.
// Labels describe the finite route tree, never the actual captured values.
var metricRoutes = []string{
	"/i/status", "/i/teams/:team", "/i/scim/users/:team/:user",
	"/i/sso/settings", "/i/scim/userinfo/:user", "/i/identity-providers/:team",
	"/sso/metadata", "/sso/metadata/:team", "/sso/initiate-login/:idp",
	"/sso/finalize-login", "/sso/finalize-login/:team", "/sso/settings", "/sso/get-by-email",
	"/identity-providers", "/identity-providers/:id", "/identity-providers/:id/raw",
	"/scim/auth-tokens", "/scim/auth-tokens/:id",
	"/scim/v2/ServiceProviderConfig", "/scim/v2/Schemas", "/scim/v2/Schemas/:id",
	"/scim/v2/ResourceTypes", "/scim/v2/Users", "/scim/v2/Users/:id",
	"/scim/v2/Groups", "/scim/v2/Groups/:id",
}

func metricRoute(path string) string {
	if path == "/i/metrics" {
		return "prometheus"
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	for _, route := range metricRoutes {
		pattern := strings.Split(strings.TrimPrefix(route, "/"), "/")
		if len(parts) != len(pattern) {
			continue
		}
		match := true
		for i, segment := range pattern {
			if !strings.HasPrefix(segment, ":") && segment != parts[i] {
				match = false
				break
			}
		}
		if match {
			return route
		}
	}
	return "N/A"
}

type histogram struct {
	buckets [12]uint64
	count   uint64
	sum     float64
}
type httpMetrics struct {
	mu          sync.Mutex
	latencies   map[string]*histogram
	connections atomic.Int64
	gcValues    [5]float64
}

func newHTTPMetrics() *httpMetrics { return &httpMetrics{latencies: map[string]*histogram{}} }

func (m *httpMetrics) observe(path, method string, status int, elapsed time.Duration) {
	normalized := metricRoute(path)
	labels := fmt.Sprintf("handler=%s,method=%s,status_code=%s", strconv.Quote(normalized), strconv.Quote(method), strconv.Quote(strconv.Itoa(status)))
	seconds := elapsed.Seconds()
	m.mu.Lock()
	defer m.mu.Unlock()
	h := m.latencies[labels]
	if h == nil {
		h = new(histogram)
		m.latencies[labels] = h
	}
	h.count++
	h.sum += seconds
	for i, upper := range latencyBounds {
		if seconds <= upper {
			h.buckets[i]++
		}
	}
}

func (m *httpMetrics) ConnState(_ net.Conn, state http.ConnState) {
	switch state {
	case http.StateNew:
		m.connections.Add(1)
	case http.StateClosed, http.StateHijacked:
		m.connections.Add(-1)
	}
}

// Runtime gauges retain Wire's existing names and units. Values describe the Go
// runtime and are sampled every 60 seconds, just like the reference collector.
func (m *httpMetrics) Collect(ctx context.Context) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	var previousCPU float64
	var previousGC uint32
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		var stats runtime.MemStats
		runtime.ReadMemStats(&stats)
		sample := []runtimemetrics.Sample{{Name: "/cpu/classes/gc/total:cpu-seconds"}, {Name: "/gc/heap/live:bytes"}}
		runtimemetrics.Read(sample)
		cpu := sample[0].Value.Float64()
		live := float64(sample[1].Value.Uint64())
		m.mu.Lock()
		m.gcValues[0] = float64(stats.TotalAlloc)
		m.gcValues[1] = math.Max(m.gcValues[1], live)
		m.gcValues[2] = live
		if stats.NumGC > previousGC {
			// Go exposes cumulative GC CPU, not the last individual cycle. This
			// gauge is the mean per cycle in the sample interval; document that
			// diagnostic limitation rather than misreporting the interval total.
			m.gcValues[3] = (cpu - previousCPU) / float64(stats.NumGC-previousGC)
			m.gcValues[4] = float64(stats.PauseNs[(stats.NumGC-1)%256]) / 1e9
		}
		m.mu.Unlock()
		previousCPU, previousGC = cpu, stats.NumGC
	}
}

func (m *httpMetrics) serve(w http.ResponseWriter) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var body strings.Builder
	body.WriteString("# HELP http_request_duration_seconds The HTTP request latencies in seconds.\n# TYPE http_request_duration_seconds histogram\n")
	keys := make([]string, 0, len(m.latencies))
	for k := range m.latencies {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, labels := range keys {
		h := m.latencies[labels]
		for i, count := range h.buckets {
			fmt.Fprintf(&body, "http_request_duration_seconds_bucket{%s,le=%q} %d\n", labels, latencyNames[i], count)
		}
		fmt.Fprintf(&body, "http_request_duration_seconds_sum{%s} %s\nhttp_request_duration_seconds_count{%s} %d\n", labels, strconv.FormatFloat(h.sum, 'g', -1, 64), labels, h.count)
	}
	fmt.Fprintf(&body, "# HELP net_connections Number of active connections\n# TYPE net_connections gauge\nnet_connections %d.0\n", m.connections.Load())
	for i, metric := range []struct{ name, help string }{
		{"gc_bytes_allocated_total", "Bytes allocated since the start of the server"},
		{"gc_bytes_used_max", "Maximum amount of memory living on the heap after the last major GC"},
		{"gc_bytes_used_current", "Number of bytes in active use as of the last GC"},
		{"gc_seconds_cpu", "CPU time spent on last GC"},
		{"gc_seconds_wall", "Wall clock time spent on last GC"},
	} {
		fmt.Fprintf(&body, "# HELP %s %s\n# TYPE %s gauge\n%s %s\n", metric.name, metric.help, metric.name, metric.name, strconv.FormatFloat(m.gcValues[i], 'g', -1, 64))
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	_, _ = w.Write([]byte(body.String()))
}
