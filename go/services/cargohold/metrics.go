package cargohold

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type histogramKey struct {
	handler, method string
	status          int
}
type requestHistogram struct {
	count   uint64
	sum     float64
	buckets [11]uint64
}
type httpMetrics struct {
	sync.Mutex
	values map[histogramKey]requestHistogram
}

var latencyBuckets = [...]float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10}
var latencyLabels = [...]string{"0.005", "0.01", "0.025", "0.05", "0.1", "0.25", "0.5", "1.0", "2.5", "5.0", "10.0"}

func (s *Server) observeHTTP(r *http.Request, status int, elapsed time.Duration) {
	key := histogramKey{metricHandler(r.URL.Path), r.Method, status}
	seconds := elapsed.Seconds()
	s.metrics.Lock()
	defer s.metrics.Unlock()
	if s.metrics.values == nil {
		s.metrics.values = make(map[histogramKey]requestHistogram)
	}
	h := s.metrics.values[key]
	h.count++
	h.sum += seconds
	for i, bound := range latencyBuckets {
		if seconds <= bound {
			h.buckets[i]++
		}
	}
	s.metrics.values[key] = h
}

// Use API route templates, never asset/user IDs, for handler labels.
func metricHandler(path string) string {
	if path == "/i/metrics" {
		return "prometheus"
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) > 0 && looksLikeVersion(parts[0]) {
		parts = parts[1:]
	}
	for _, template := range []string{
		"i/status", "i/assets/:key", "federation/get-asset", "federation/stream-asset",
		"bot/assets", "bot/assets/:key", "provider/assets", "provider/assets/:key",
		"assets/v3", "assets/v3/:key/token", "assets/v3/:key", "assets/v4/:key_domain/:key",
		"assets/:key/token", "assets/:key_domain/:key", "assets",
		"assets/:id", "conversations/:conv/assets/:id", "conversations/:conv/otr/assets/:id",
	} {
		segments := strings.Split(template, "/")
		if len(segments) != len(parts) {
			continue
		}
		match := true
		for i, segment := range segments {
			if !strings.HasPrefix(segment, ":") && segment != parts[i] {
				match = false
				break
			}
		}
		if match {
			return "/" + template
		}
	}
	return "N/A"
}

func (s *Server) ConnectionState(_ net.Conn, state http.ConnState) {
	switch state {
	case http.StateNew:
		s.connections.Add(1)
	case http.StateClosed, http.StateHijacked:
		s.connections.Add(-1)
	}
}

func (s *Server) writeMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	fmt.Fprintf(w, "# HELP net_s3_upload_size Number of bytes uploaded successfully uploaded to S3\n# TYPE net_s3_upload_size counter\nnet_s3_upload_size %d\n# HELP net_s3_upload_ok Number of successful S3 Uploads\n# TYPE net_s3_upload_ok counter\nnet_s3_upload_ok %d\n", s.bytes.Load(), s.uploads.Load())
	fmt.Fprint(w, "# HELP http_request_duration_seconds The HTTP request latencies in seconds.\n# TYPE http_request_duration_seconds histogram\n")
	s.metrics.Lock()
	keys := make([]histogramKey, 0, len(s.metrics.values))
	for key := range s.metrics.values {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a.handler != b.handler {
			return a.handler < b.handler
		}
		if a.method != b.method {
			return a.method < b.method
		}
		return a.status < b.status
	})
	for _, key := range keys {
		h := s.metrics.values[key]
		labels := "handler=" + strconv.Quote(key.handler) + ",method=" + strconv.Quote(key.method) + ",status_code=" + strconv.Quote(strconv.Itoa(key.status))
		for i, limit := range latencyLabels {
			fmt.Fprintf(w, "http_request_duration_seconds_bucket{%s,le=%q} %d\n", labels, limit, h.buckets[i])
		}
		fmt.Fprintf(w, "http_request_duration_seconds_bucket{%s,le=\"+Inf\"} %d\nhttp_request_duration_seconds_sum{%s} %g\nhttp_request_duration_seconds_count{%s} %d\n", labels, h.count, labels, h.sum, labels, h.count)
	}
	s.metrics.Unlock()
	remaining := 0.0
	if provider, ok := s.Store.(interface{ CredentialLifetime(context.Context) float64 }); ok {
		remaining = provider.CredentialLifetime(r.Context())
	}
	fmt.Fprintf(w, "# HELP net_connections Number of active connections\n# TYPE net_connections gauge\nnet_connections %d\n# HELP aws_auth_token_secs_remaining Number of seconds left before AWS Auth expires\n# TYPE aws_auth_token_secs_remaining gauge\naws_auth_token_secs_remaining %g\n", s.connections.Load(), remaining)
}

func (s *S3Store) CredentialLifetime(ctx context.Context) float64 {
	credentials, err := s.config.Credentials.Retrieve(ctx)
	if err != nil || !credentials.CanExpire {
		return 0
	}
	return time.Until(credentials.Expires).Seconds()
}
