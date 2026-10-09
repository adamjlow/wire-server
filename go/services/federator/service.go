// SPDX-License-Identifier: AGPL-3.0-only
package federator

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
)

type Service struct {
	operational        *Metrics
	config             Config
	local              *http.Client
	remote             atomic.Pointer[http.Transport]
	mu                 sync.Mutex
	incoming, outgoing map[string]uint64
	requests           uint64
	duration           float64
}

func (s *Service) diagnostic(stage, domain, rpc string, status int, label string) {
	if s.config.LogLevel != "Debug" && s.config.LogLevel != "Trace" {
		return
	}
	// Temporary regression trace: no payload, request ID, certificates or raw errors.
	if len(domain) > 253 {
		domain = domain[:253]
	}
	if len(rpc) > 128 {
		rpc = rpc[:128]
	}
	if len(label) > 64 {
		label = label[:64]
	}
	line, _ := json.Marshal(map[string]any{"event": "federation-diagnostic", "stage": stage, "domain": domain, "rpc": rpc, "status": status, "label": label})
	_, _ = fmt.Fprintln(os.Stderr, string(line))
}

func New(c Config) (*Service, error) {
	tc, _, err := c.Settings.tlsConfig()
	if err != nil {
		return nil, err
	}
	s := &Service{config: c, incoming: map[string]uint64{}, outgoing: map[string]uint64{}}
	s.local = &http.Client{Transport: &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: time.Duration(*c.Settings.TCPConnectionTimeout) * time.Microsecond}).DialContext, MaxIdleConns: 4096, MaxIdleConnsPerHost: 1024, ResponseHeaderTimeout: 10 * time.Second, DisableCompression: true}, CheckRedirect: noRedirect}
	s.operational = newMetrics()
	s.remote.Store(s.transport(tc))
	return s, nil
}
func noRedirect(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
func (s *Service) transport(c *tls.Config) *http.Transport {
	return &http.Transport{Proxy: nil, TLSClientConfig: c, ForceAttemptHTTP2: true, DisableKeepAlives: true, DisableCompression: true, DialContext: (&net.Dialer{Timeout: time.Duration(*s.config.Settings.TCPConnectionTimeout) * time.Microsecond}).DialContext, TLSHandshakeTimeout: time.Duration(*s.config.Settings.TCPConnectionTimeout) * time.Microsecond}
}
func (s *Service) monitor(ctx context.Context) {
	_, previous, _ := s.config.Settings.tlsConfig()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c, hash, err := s.config.Settings.tlsConfig()
			if err == nil && hash != previous {
				old := s.remote.Swap(s.transport(c))
				old.CloseIdleConnections()
				previous = hash
			}
		}
	}
}
func (s *Service) Run(ctx context.Context) error {
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)
	internal := &http.Server{Addr: net.JoinHostPort("", fmt.Sprint(s.config.Internal.Port)), Handler: s.Handler(false), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 75 * time.Second, MaxHeaderBytes: 1024 * 1024, Protocols: protocols}
	external := &http.Server{Addr: net.JoinHostPort("", fmt.Sprint(s.config.External.Port)), Handler: s.Handler(true), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 75 * time.Second, MaxHeaderBytes: 1024 * 1024, Protocols: protocols}
	// Bind both listeners before announcing health, avoiding a half-started process.
	l1, err := net.Listen("tcp", internal.Addr)
	if err != nil {
		return err
	}
	defer l1.Close()
	l2, err := net.Listen("tcp", external.Addr)
	if err != nil {
		return err
	}
	defer l2.Close()
	monitorCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.monitor(monitorCtx)
	go s.operational.Collect(monitorCtx)
	internal.ConnState = s.operational.ConnState
	external.ConnState = s.operational.ConnState
	done := make(chan error, 2)
	go func() { done <- internal.Serve(l1) }()
	go func() { done <- external.Serve(l2) }()
	select {
	case <-ctx.Done():
	case err = <-done:
	}
	drain, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	var wg sync.WaitGroup
	for _, srv := range []*http.Server{internal, external} {
		wg.Go(func() {
			if e := srv.Shutdown(drain); e != nil {
				_ = srv.Close()
			}
		})
	}
	wg.Wait()
	s.local.CloseIdleConnections()
	s.remote.Load().CloseIdleConnections()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
func (s *Service) Handler(external bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		{
			observed := &observedWriter{ResponseWriter: w}
			w = observed
			defer func() {
				status := observed.status
				if status == 0 {
					status = 200
				}
				s.operational.observe(r.URL.Path, r.Method, status, time.Since(started))
			}()
		}
		defer func() { s.mu.Lock(); s.requests++; s.duration += time.Since(started).Seconds(); s.mu.Unlock() }()
		if r.URL.Path == "/i/metrics" {
			s.metrics(w)
			return
		}
		if r.URL.Path == "/i/status" {
			if r.Method != "GET" {
				raw(w, 405, "")
				return
			}
			standalone, ok := r.URL.Query()["standalone"]
			flag := ok && (len(standalone) == 0 || standalone[0] == "" || standalone[0] == "true" || standalone[0] == "1")
			if !flag {
				port := s.config.External.Port
				if external {
					port = s.config.Internal.Port
				}
				req, _ := http.NewRequestWithContext(r.Context(), "GET", "http://localhost:"+fmt.Sprint(port)+"/i/status?standalone", nil)
				resp, err := s.local.Do(req)
				if err != nil {
					serverError(w)
					return
				}
				_ = resp.Body.Close()
				if resp.StatusCode < 200 || resp.StatusCode >= 300 {
					raw(w, 500, "other server unavailable")
					return
				}
			}
			w.Header().Set("Content-Type", "text/plain;charset=utf-8")
			w.WriteHeader(200)
			return
		}
		segments := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
		expected := 4
		prefix := "rpc"
		if external {
			expected = 3
			prefix = "federation"
		}
		if len(segments) != expected || segments[0] != prefix || segments[len(segments)-1] == "" {
			raw(w, 404, "")
			return
		}
		component, rpc := segments[len(segments)-2], segments[len(segments)-1]
		if component != "brig" && component != "galley" && component != "cargohold" {
			raw(w, 400, "Invalid component: "+component)
			return
		}
		if !validRPC(rpc) {
			raw(w, 400, "invalid-endpoint")
			return
		}
		domain := ""
		var cert *x509.Certificate
		if external {
			origin, ok := r.Header[http.CanonicalHeaderKey("Wire-Origin-Domain")]
			if !ok || len(origin) == 0 {
				raw(w, 400, "Header Wire-Origin-Domain is required")
				return
			}
			domain = strings.ToLower(origin[0])
			if !validDomain(domain) {
				raw(w, 400, "Error parsing header Wire-Origin-Domain failed: invalid domain")
				return
			}
			header, ok := r.Header[http.CanonicalHeaderKey("X-SSL-Certificate")]
			if !ok || len(header) == 0 {
				raw(w, 400, "Header X-SSL-Certificate is required")
				return
			}
			var err error
			cert, err = parseCertificate(header[0])
			if err != nil {
				raw(w, 400, "Error parsing header X-SSL-Certificate failed: "+err.Error())
				return
			}
		} else {
			domain = strings.ToLower(segments[1])
			if !validDomain(domain) {
				raw(w, 400, "invalid domain")
				return
			}
		}
		if external {
			s.increment(true, domain)
		}
		if r.Method != "POST" || r.URL.RawQuery != "" || r.URL.ForceQuery {
			writeError(w, 403, "invalid-endpoint", "The requested endpoint does not exist", nil)
			return
		}
		ridValues, present := r.Header[http.CanonicalHeaderKey("Wire-Origin-Request-Id")]
		rid := ""
		if present {
			if len(ridValues) > 0 {
				rid = ridValues[0]
			}
		} else {
			var b [16]byte
			if _, err := rand.Read(b[:]); err != nil {
				serverError(w)
				return
			}
			b[6] = (b[6] & 0x0f) | 0x40
			b[8] = (b[8] & 0x3f) | 0x80
			h := hex.EncodeToString(b[:])
			rid = h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
			r.Header = r.Header.Clone()
			r.Header.Set("Wire-Origin-Request-Id", rid)
		}
		if !s.allow(r.Context(), domain, rid, w) {
			return
		}
		if external {
			// validateDomain performs its own live policy read in the reference.
			if !s.allow(r.Context(), domain, rid, w) {
				return
			}
			targets, err := dnsTargets(r.Context(), s.config.Settings.dnsAddress(), domain)
			if !discovery(w, domain, targets, err) {
				return
			}
			matched := false
			var names []string
			for _, target := range targets {
				name := strings.TrimSuffix(target.Host, ".")
				names = append(names, fmt.Sprintf("[NameMismatch %q]", name))
				if certificateMatches(cert, name) {
					matched = true
				}
			}
			if !matched {
				writeError(w, 403, "authentication-failure", "none of the domain names match the certificate, errors: ["+strings.Join(names, ",")+"]", nil)
				return
			}
			s.inward(w, r, domain, component, rpc, rid)
		} else {
			s.increment(false, domain)
			s.outward(w, r, domain, component, rpc, rid)
		}
	})
}
func (s *Service) increment(incoming bool, domain string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if incoming {
		s.incoming[domain]++
	} else {
		s.outgoing[domain]++
	}
}
func validRPC(v string) bool {
	if v == "" {
		return false
	}
	for _, c := range v {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || unicode.IsNumber(c) || c == '_' || c == '-') {
			return false
		}
	}
	return true
}
func validDomain(v string) bool {
	if len(v) > 253 || v == "" {
		return false
	}
	parts := strings.Split(v, ".")
	if len(parts) < 2 && v != "localhost" {
		return false
	}
	for _, p := range parts {
		if len(p) == 0 || len(p) > 63 || p[0] == '-' || p[len(p)-1] == '-' {
			return false
		}
		for _, c := range p {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	last := parts[len(parts)-1]
	return last[0] < '0' || last[0] > '9'
}
func parseCertificate(v string) (*x509.Certificate, error) {
	decoded, err := url.QueryUnescape(v)
	if err != nil {
		return nil, errors.New("invalid certificate encoding")
	}
	block, rest := pem.Decode([]byte(decoded))
	if block == nil {
		return nil, errors.New("no certificate found")
	}
	if b, _ := pem.Decode(rest); b != nil {
		return nil, errors.New("found multiple certificates")
	}
	c, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, errors.New("invalid certificate")
	}
	return c, nil
}
func certificateMatches(c *x509.Certificate, host string) bool {
	if len(c.DNSNames) > 0 {
		return c.VerifyHostname(host) == nil
	}
	pattern := strings.ToLower(c.Subject.CommonName)
	host = strings.ToLower(host)
	if strings.HasPrefix(pattern, "*.") {
		return strings.Contains(host, ".") && strings.SplitN(host, ".", 2)[1] == pattern[2:]
	}
	return host == pattern
}
func (s *Service) allow(ctx context.Context, domain, rid string, w http.ResponseWriter) bool {
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://"+s.config.Brig.address()+"/i/federation/remotes", nil)
	req.Header.Set("Request-Id", rid)
	resp, err := s.local.Do(req)
	if err != nil {
		s.diagnostic("policy-transport", domain, "", 500, fmt.Sprintf("%T", err))
		serverError(w)
		return false
	}
	defer resp.Body.Close()
	var policy struct {
		Strategy *string `json:"strategy"`
		Remotes  *[]struct {
			Domain       string `json:"domain"`
			SearchPolicy string `json:"search_policy"`
			Restriction  struct {
				Tag   string          `json:"tag"`
				Value json.RawMessage `json:"value"`
			} `json:"restriction"`
		} `json:"remotes"`
		Interval *int64 `json:"update_interval"`
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024+1))
	if resp.StatusCode != 200 || readErr != nil || len(body) > 8*1024*1024 || json.Unmarshal(body, &policy) != nil || policy.Strategy == nil || policy.Remotes == nil || policy.Interval == nil {
		s.diagnostic("policy-response", domain, "", resp.StatusCode, "invalid-policy")
		serverError(w)
		return false
	}
	if !member(*policy.Strategy, []string{"allowAll", "allowNone", "allowDynamic"}) {
		serverError(w)
		return false
	}
	for _, remote := range *policy.Remotes {
		if !validDomain(strings.ToLower(remote.Domain)) || !member(remote.SearchPolicy, []string{"no_search", "exact_handle_search", "full_search"}) || len(remote.Restriction.Value) == 0 {
			serverError(w)
			return false
		}
		switch remote.Restriction.Tag {
		case "allow_all":
			if string(remote.Restriction.Value) != "null" {
				serverError(w)
				return false
			}
		case "restrict_by_team":
			var teams []string
			if json.Unmarshal(remote.Restriction.Value, &teams) != nil || string(remote.Restriction.Value) == "null" {
				serverError(w)
				return false
			}
		default:
			serverError(w)
			return false
		}
	}
	allowed := *policy.Strategy == "allowAll"
	if *policy.Strategy == "allowDynamic" {
		for _, remote := range *policy.Remotes {
			if strings.ToLower(remote.Domain) == domain {
				allowed = true
			}
		}
	}
	if !allowed {
		s.diagnostic("policy-denied", domain, "", 400, "federation-denied")
		writeError(w, 400, "federation-denied", "origin domain ["+domain+"] not in the federation allow list", nil)
	}
	return allowed
}
func discovery(w http.ResponseWriter, domain string, targets []Target, err error) bool {
	name := "_wire-server-federator._tcp." + domain
	if err != nil {
		writeError(w, 400, "discovery-failure", "DNS error: "+name, nil)
		return false
	}
	if len(targets) == 0 {
		writeError(w, 422, "invalid-domain", "srv record not found: "+name, nil)
		return false
	}
	return true
}
func (s *Service) inward(w http.ResponseWriter, r *http.Request, domain, component, rpc, rid string) {
	endpoint := s.config.Brig
	switch component {
	case "cargohold":
		endpoint = s.config.Cargohold
	case "galley":
		endpoint = s.config.Galley
	}
	req, err := http.NewRequestWithContext(r.Context(), "POST", "http://"+endpoint.address()+"/federation/"+url.PathEscape(rpc), r.Body)
	if err != nil {
		serverError(w)
		return
	}
	req.ContentLength = r.ContentLength
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Wire-Origin-Domain", domain)
	req.Header.Set("Request-Id", rid)
	for _, value := range r.Header.Values("X-Wire-API-Version") {
		req.Header.Add("X-Wire-API-Version", value)
	}
	if value := originIP(r.Header.Get("X-Forwarded-For")); value != "" {
		req.Header.Set("Wire-Origin-IP", value)
	}
	resp, err := s.local.Do(req)
	if err != nil {
		s.diagnostic("inward-transport", domain, rpc, 500, fmt.Sprintf("%T", err))
		serverError(w)
		return
	}
	s.diagnostic("inward-response", domain, rpc, resp.StatusCode, resp.Header.Get("Content-Encoding"))
	defer resp.Body.Close()
	if v := resp.Header.Get("Content-Type"); v != "" {
		w.Header().Set("Content-Type", v)
	} else {
		w.Header()["Content-Type"] = nil
	}
	w.WriteHeader(resp.StatusCode)
	stream(w, resp.Body)
}
func originIP(header string) string {
	for _, v := range strings.Split(header, ",") {
		v = strings.TrimSpace(v)
		if net.ParseIP(v) != nil {
			return v
		}
		if host, _, err := net.SplitHostPort(v); err == nil && net.ParseIP(host) != nil {
			return host
		}
		if net.ParseIP(strings.Trim(v, "[]")) != nil {
			return strings.Trim(v, "[]")
		}
	}
	return ""
}
func (s *Service) outward(w http.ResponseWriter, r *http.Request, domain, component, rpc, rid string) {
	targets, err := dnsTargets(r.Context(), s.config.Settings.dnsAddress(), domain)
	if !discovery(w, domain, targets, err) {
		return
	}
	target := targets[0]
	host := strings.TrimSuffix(target.Host, ".")
	path := "/federation/" + component + "/" + url.PathEscape(rpc)
	req, err := http.NewRequestWithContext(r.Context(), "POST", "https://"+net.JoinHostPort(host, fmt.Sprint(target.Port))+path, r.Body)
	if err != nil {
		serverError(w)
		return
	}
	req.ContentLength = r.ContentLength
	req.Header = r.Header.Clone()
	req.Header.Del("Host")
	req.Header.Set("Request-Id", rid)
	for _, key := range []string{"Connection", "Proxy-Connection", "Keep-Alive", "Transfer-Encoding", "Upgrade"} {
		req.Header.Del(key)
	}
	ip := ""
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) { ip, _, _ = net.SplitHostPort(info.Conn.RemoteAddr().String()) }}))
	client := http.Client{Transport: s.remote.Load(), CheckRedirect: noRedirect}
	resp, err := client.Do(req)
	var data any
	if validDomain(strings.ToLower(target.Host)) {
		data = map[string]any{"type": "federation", "domain": strings.ToLower(target.Host), "path": path}
	}
	if err != nil {
		status, label := 521, "federation-connection-refused"
		var certErr *tls.CertificateVerificationError
		var tlsErr tls.RecordHeaderError
		if errors.As(err, &certErr) || errors.As(err, &tlsErr) || strings.Contains(err.Error(), "tls:") {
			status, label = 525, "federation-tls-error"
		}
		category := fmt.Sprintf("%T", errors.Unwrap(err))
		for _, marker := range []string{"client conn not usable", "GOAWAY", "REFUSED_STREAM", "unexpected EOF", "EOF", "connection reset", "connection refused", "context canceled", "deadline exceeded", "timeout"} {
			if strings.Contains(err.Error(), marker) {
				category = marker
				break
			}
		}
		s.diagnostic("outward-transport", domain, rpc, status, category)
		writeError(w, status, label, "Remote federation transport failed", map[string]any{"data": data})
		return
	}
	s.diagnostic("outward-response", domain, rpc, resp.StatusCode, resp.Header.Get("Content-Encoding"))
	defer resp.Body.Close()
	if resp.ProtoMajor != 2 {
		writeError(w, 533, "federation-http2-error", "Remote federation protocol failed", map[string]any{"data": data})
		return
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, e := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024))
		if e != nil {
			serverError(w)
			return
		}
		var inner map[string]any
		if json.Unmarshal(body, &inner) != nil || inner["code"] == nil || inner["label"] == nil || inner["message"] == nil {
			inner = map[string]any{"code": resp.StatusCode, "label": "unknown-error", "message": strings.ToValidUTF8(string(body), "�")}
		}
		s.diagnostic("outward-remote-error", domain, rpc, resp.StatusCode, fmt.Sprint(inner["label"]))
		writeError(w, 533, "federation-remote-error", "A remote federator failed with status code: "+strconv.Itoa(resp.StatusCode), map[string]any{"data": data, "inner": inner})
		return
	}
	for key, values := range resp.Header {
		if strings.EqualFold(key, "Transfer-Encoding") || strings.EqualFold(key, "Connection") {
			continue
		}
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	if ip != "" {
		w.Header().Add("Wire-Remote-IP", ip)
	}
	w.WriteHeader(resp.StatusCode)
	stream(w, resp.Body)
}
func stream(w http.ResponseWriter, body io.Reader) {
	buf := make([]byte, 32*1024)
	for {
		n, err := body.Read(buf)
		if n > 0 {
			if _, e := w.Write(buf[:n]); e != nil {
				return
			}
			_ = http.NewResponseController(w).Flush()
		}
		if err != nil {
			return
		}
	}
}
func raw(w http.ResponseWriter, status int, text string) {
	w.Header()["Content-Type"] = nil
	w.WriteHeader(status)
	_, _ = io.WriteString(w, text)
}
func serverError(w http.ResponseWriter) {
	writeError(w, 500, "server-error", "Internal Server Error", nil)
}
func writeError(w http.ResponseWriter, status int, label, message string, extra map[string]any) {
	value := map[string]any{"code": status, "label": label, "message": message}
	for k, v := range extra {
		if v != nil {
			value[k] = v
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	data, _ := json.Marshal(value)
	_, _ = w.Write(data)
}
func (s *Service) metrics(w http.ResponseWriter) {
	s.operational.serve(w)
	s.mu.Lock()
	defer s.mu.Unlock()
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = io.WriteString(w, "# TYPE com_wire_federator_incoming_requests counter\n")
	for domain, count := range s.incoming {
		_, _ = fmt.Fprintf(w, "com_wire_federator_incoming_requests{origin_domain=%q} %d\n", domain, count)
	}
	_, _ = io.WriteString(w, "# TYPE com_wire_federator_outgoing_requests counter\n")
	for domain, count := range s.outgoing {
		_, _ = fmt.Fprintf(w, "com_wire_federator_outgoing_requests{target_domain=%q} %d\n", domain, count)
	}
}
