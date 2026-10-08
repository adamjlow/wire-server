package gundeck

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/redis/go-redis/v9"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Server struct {
	Config           Config
	redis            redis.UniversalClient
	additional       redis.UniversalClient
	logger           *Logger
	cassandra        *gocql.Session
	http             *http.Client
	aws              aws.Config
	sns              *sns.Client
	tokenMu          sync.Mutex
	rabbitMu         sync.Mutex
	rabbit           *amqp.Connection
	rabbitChan       *amqp.Channel
	background       context.Context
	cancelBackground context.CancelFunc
	deliverySlots    chan struct{}
	deliveryWG       sync.WaitGroup
	feedbackWG       sync.WaitGroup
	awsHTTP          *http.Client
	queueURL         string
	metricsMu        sync.Mutex
	counters         map[string]uint64
	budgetMu         sync.Mutex
	budgetSpent      int
}

func NewServer(c Config) (*Server, error) {
	r, err := openRedis(c.Redis, false)
	if err != nil {
		return nil, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = max(c.Settings.HTTPPool, 1)
	transport.MaxIdleConnsPerHost = max(c.Settings.HTTPPool, 1)
	s := &Server{Config: c, redis: r, http: &http.Client{Timeout: 10 * time.Second, Transport: transport}}
	s.cassandra, err = openCassandra(c)
	if err != nil {
		_ = r.Close()
		return nil, err
	}
	if c.Additional != nil {
		s.additional, err = openRedis(*c.Additional, true)
		if err != nil {
			_ = r.Close()
			s.cassandra.Close()
			return nil, err
		}
	}
	s.logger = NewLogger(c, os.Stdout)
	s.background, s.cancelBackground = context.WithCancel(context.Background())
	s.deliverySlots = make(chan struct{}, 1024)
	s.counters = map[string]uint64{}
	s.awsHTTP = &http.Client{Transport: transport, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s.aws, s.sns, err = openAWS(ctx, c, s.awsHTTP)
	if err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}
func (s *Server) Close() {
	if s.cancelBackground != nil {
		s.cancelBackground()
	}
	s.feedbackWG.Wait()
	s.deliveryWG.Wait()
	if s.rabbit != nil {
		_ = s.rabbit.Close()
	}
	if s.cassandra != nil {
		s.cassandra.Close()
	}
	s.http.CloseIdleConnections()
	if s.awsHTTP != nil {
		s.awsHTTP.CloseIdleConnections()
	}
	_ = s.redis.Close()
	if s.additional != nil {
		_ = s.additional.Close()
	}
}
func uuidValid(v string) bool {
	if len(v) != 36 {
		return false
	}
	for i, c := range v {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
			continue
		}
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}
func clientValid(v string) bool {
	if v == "" || len(strings.TrimLeft(v, "0")) > 16 {
		return false
	}
	for _, c := range v {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}
func plain(w http.ResponseWriter, status int, body string) {
	w.Header()["Content-Type"] = nil
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}
func jsonResponse(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json;charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func failure(w http.ResponseWriter, status int, label, message string) {
	jsonResponse(w, status, map[string]any{"code": status, "label": label, "message": message})
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	r.Header.Del("X-Wire-API-Version")
	r = r.WithContext(context.WithValue(r.Context(), requestIDKey{}, r.Header.Get("Request-Id")))
	if r.Header.Get("Content-Encoding") == "gzip" {
		original := r.Body
		compressed, err := gzip.NewReader(original)
		if err != nil {
			plain(w, 400, "Invalid compressed JSON")
			return
		}
		defer original.Close()
		defer compressed.Close()
		r.Body = io.NopCloser(compressed)
		r.Header.Del("Content-Encoding")
	}
	path := r.URL.Path
	version := 0
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) > 1 && strings.HasPrefix(parts[0], "v") {
		parsed, err := strconv.Atoi(parts[0][1:])
		if err != nil || parsed < 0 || parsed > 19 || s.disabled(parsed) {
			failure(w, 404, "unsupported-version", "Unsupported version")
			return
		}
		version = parsed
		path = "/" + strings.Join(parts[1:], "/")
		if strings.HasPrefix(path, "/i/") {
			failure(w, 404, "unsupported-version", "Internal APIs (`/i/...`) are not under version control")
			return
		}
	}
	response := &gzipResponse{ResponseWriter: w, accepted: gzipAccepted(r)}
	defer response.Close()
	w = response
	switch {
	case path == "/i/metrics" && r.Method == "GET":
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		s.metricsMu.Lock()
		for name, value := range s.counters {
			_, _ = fmt.Fprintf(w, "# TYPE %s counter\n%s %d\n", name, name, value)
		}
		s.metricsMu.Unlock()
	case strings.HasPrefix(path, "/i/push-tokens/") && r.Method == "GET":
		s.tokensHTTP(w, r, path)
	case strings.HasPrefix(path, "/i/users/") && strings.HasSuffix(path, "/consumable-notifications") && r.Method == "POST":
		parts := strings.Split(strings.TrimPrefix(path, "/i/users/"), "/")
		if len(parts) != 4 || parts[1] != "clients" {
			plain(w, 404, "")
			return
		}
		if !uuidValid(parts[0]) {
			plain(w, 400, "invalid UUID")
			return
		}
		if !clientValid(parts[2]) {
			plain(w, 400, "Failed reading: takeWhile1")
			return
		}
		if err := s.registerConsumable(r.Context(), strings.ToLower(parts[0]), canonicalClient(parts[2])); err != nil {
			failure(w, 500, "server-error", "Server Error")
			return
		}
		plain(w, 204, "")
	case strings.HasPrefix(path, "/i/clients/") && r.Method == "DELETE":
		user, ok := principal(w, r)
		if !ok {
			return
		}
		client := strings.TrimPrefix(path, "/i/clients/")
		if !clientValid(client) {
			plain(w, 400, "Failed reading: takeWhile1")
			return
		}
		addresses, err := s.lookupTokens(r.Context(), user, false)
		if err != nil {
			tokenFailure(w, err)
			return
		}
		selected := []pushAddress{}
		for _, address := range addresses {
			if address.Client == canonicalClient(client) {
				selected = append(selected, address)
			}
		}
		s.tokenMu.Lock()
		err = s.deleteTokens(r.Context(), selected, nil)
		s.tokenMu.Unlock()
		if err != nil {
			tokenFailure(w, err)
			return
		}
		emptyJSON(w, 200)
	case path == "/i/push/v2" && r.Method == "POST":
		s.pushHTTP(w, r)
	case path == "/i/status" && r.Method == "GET":
		emptyJSON(w, 200)
	case (path == "/push/tokens" && (r.Method == "GET" || r.Method == "POST")) || (strings.HasPrefix(path, "/push/tokens/") && r.Method == "DELETE"):
		s.tokensHTTP(w, r, path)
	case (path == "/notifications" || strings.HasPrefix(path, "/notifications/")) && r.Method == "GET":
		s.notificationHTTP(w, r, path, version)
	case path == "/i/user" && r.Method == "DELETE":
		user, ok := principal(w, r)
		if !ok {
			return
		}
		addresses, err := s.lookupTokens(r.Context(), user, false)
		if err != nil {
			tokenFailure(w, err)
			return
		}
		s.tokenMu.Lock()
		err = s.deleteTokens(r.Context(), addresses, nil)
		s.tokenMu.Unlock()
		if err != nil {
			tokenFailure(w, err)
			return
		}
		uid, _ := gocql.ParseUUID(user)
		if err = s.query(r.Context(), "DELETE FROM user_push WHERE usr = ?", uid).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5}).Exec(); err != nil {
			tokenFailure(w, err)
			return
		}
		if err := s.query(r.Context(), "DELETE FROM notifications WHERE user = ?", uid).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5}).Exec(); err != nil {
			failure(w, 500, "server-error", "Server Error")
			return
		}
		emptyJSON(w, 200)
	case path == "/i/presences" && r.Method == "POST":
		var p Presence
		if err := decodeJSON(r.Body, &p); err != nil {
			plain(w, 400, presenceJSONError(err))
			return
		}
		if !uuidValid(p.User) || p.Device == "" {
			plain(w, 400, "Invalid presence")
			return
		}
		resource, err := url.Parse(p.Resource)
		if err != nil || resource.Scheme == "" || (p.Client != nil && !clientValid(*p.Client)) {
			plain(w, 400, "Invalid presence")
			return
		}
		p.User = strings.ToLower(p.User)
		if p.Client != nil {
			value := canonicalClient(*p.Client)
			p.Client = &value
		}
		if err = s.addPresence(r.Context(), p); err != nil {
			failure(w, 500, "server-error", "Server Error")
			return
		}
		w.Header().Set("Location", p.Resource)
		emptyJSON(w, 201)
	case strings.HasPrefix(path, "/i/presences/") && r.Method == "DELETE":
		parts := strings.Split(strings.TrimPrefix(path, "/i/presences/"), "/")
		if len(parts) != 5 || parts[1] != "devices" || parts[3] != "cannons" || parts[2] == "" || parts[4] == "" {
			plain(w, 404, "")
			return
		}
		if !uuidValid(parts[0]) {
			plain(w, 400, "invalid UUID")
			return
		}
		emptyJSON(w, 200)
	case (path == "/i/presences" || strings.HasPrefix(path, "/i/presences/")) && r.Method == "GET":
		ids := []string{strings.TrimPrefix(path, "/i/presences/")}
		if path == "/i/presences" {
			value, ok := r.URL.Query()["ids"]
			if !ok {
				plain(w, 400, "Query parameter ids is required")
				return
			}
			ids = strings.Split(value[0], ",")
		}
		all := []Presence{}
		for _, id := range ids {
			if !uuidValid(id) {
				if path == "/i/presences" {
					plain(w, 400, "Error parsing query parameter ids failed: Failed reading: [\"hexadecimal digit\"]Failed reading: satisfyWith")
				} else {
					plain(w, 400, "invalid UUID")
				}
				return
			}
			presences, err := s.listPresence(r.Context(), strings.ToLower(id))
			if err != nil {
				failure(w, 500, "server-error", "Server Error")
				return
			}
			all = append(all, presences...)
		}
		jsonResponse(w, 200, all)
	case path == "/time" && version >= 9 && r.Method == "GET":
		if _, ok := principal(w, r); !ok {
			return
		}
		jsonResponse(w, 200, map[string]string{"time": time.Now().UTC().Format("2006-01-02T15:04:05Z")})
	default:
		plain(w, 404, "")
	}
}
func principal(w http.ResponseWriter, r *http.Request) (string, bool) {
	u, ok := r.Header["Z-User"]
	if !ok {
		plain(w, 400, "Header Z-User is required")
		return "", false
	}
	if len(u) == 0 || !uuidValid(u[0]) {
		plain(w, 400, "Error parsing header Z-User failed: invalid UUID")
		return "", false
	}
	return strings.ToLower(u[0]), true
}
func (s *Server) Ready(ctx context.Context) error {
	if err := s.redis.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("Redis initialization failed")
	}
	var version int32
	if err := s.query(ctx, "SELECT version FROM meta WHERE id=1 ORDER BY version DESC LIMIT 1").Scan(&version); err != nil || version < 12 {
		return fmt.Errorf("Cassandra schema version must be at least 12")
	}
	s.rabbitMu.Lock()
	_, err := s.rabbitChannel()
	s.rabbitMu.Unlock()
	if err != nil {
		return err
	}
	if err = s.openFeedback(ctx); err != nil {
		return err
	}
	s.feedbackWG.Go(s.listenFeedback)
	return nil
}

func emptyJSON(w http.ResponseWriter, status int) {
	w.Header().Set("Content-Type", "application/json;charset=utf-8")
	w.WriteHeader(status)
}
func presenceJSONError(err error) string {
	if err == io.ErrUnexpectedEOF {
		return "Unexpected end-of-input, expecting record key literal or }"
	}
	return "Invalid presence JSON"
}

func (s *Server) pushHTTP(w http.ResponseWriter, r *http.Request) {
	var input io.Reader = r.Body
	if r.Header.Get("Content-Encoding") == "gzip" {
		compressed, err := gzip.NewReader(r.Body)
		if err != nil {
			plain(w, 400, "Invalid compressed JSON")
			return
		}
		defer compressed.Close()
		input = compressed
	}
	raw, err := io.ReadAll(input)
	if err != nil {
		failure(w, 500, "server-error", "Server Error")
		return
	}
	trimmed := bytes.TrimSpace(raw)
	if bytes.Equal(trimmed, []byte("{")) {
		plain(w, 400, "Unexpected end-of-input, expecting record key literal or }")
		return
	}
	if len(trimmed) > 0 && trimmed[0] == '{' {
		plain(w, 400, "Error in $: parsing [] failed, expected Array, but encountered Object")
		return
	}
	pushes, err := validatePushes(raw)
	if err != nil {
		plain(w, 400, "Invalid push JSON")
		return
	}
	if err = s.pushAll(r.Context(), pushes); err != nil {
		failure(w, 500, "server-error", "Server Error")
		return
	}

	emptyJSON(w, 200)
}

func (s *Server) count(name string) { s.metricsMu.Lock(); s.counters[name]++; s.metricsMu.Unlock() }
func (s *Server) Drain(ctx context.Context) error {
	done := make(chan struct{})
	go func() { s.deliveryWG.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		s.cancelBackground()
		return ctx.Err()
	}
}

type requestIDKey struct{}

func (s *Server) disabled(version int) bool {
	for _, v := range s.Config.Settings.Disabled {
		if v == "v"+strconv.Itoa(version) || v == "development" && version == 19 {
			return true
		}
	}
	return false
}

func canonicalClient(v string) string {
	v = strings.TrimLeft(strings.ToLower(v), "0")
	if v == "" {
		return "0"
	}
	return v
}
