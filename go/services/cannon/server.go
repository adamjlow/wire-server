package cannon

import (
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type socketKey struct{ user, connection string }
type liveSocket struct {
	conn *websocket.Conn
	mu   sync.Mutex
	done chan struct{}
}

func (c *liveSocket) write(kind int, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return c.conn.WriteMessage(kind, data)
}
func (c *liveSocket) close(code int, reason string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, reason), time.Now().Add(time.Second))
}

type Server struct {
	metrics  *Metrics
	logger   *Logger
	backends *Backends
	pool     *rabbitPool
	Config   Config
	client   *http.Client
	mu       sync.Mutex
	sockets  map[socketKey]*liveSocket
}

func NewServer(c Config) *Server {
	return &Server{metrics: newMetrics(), logger: NewLogger(c, os.Stdout), Config: c, client: &http.Client{Timeout: 10 * time.Second}, sockets: make(map[socketKey]*liveSocket)}
}
func isUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i, c := range value {
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
func plain(w http.ResponseWriter, r *http.Request, status int, body string) {
	w.Header()["Content-Type"] = nil
	w.WriteHeader(status)
	if r.Method != "HEAD" {
		_, _ = io.WriteString(w, body)
	}
}
func jsonResponse(w http.ResponseWriter, r *http.Request, status int, value any) {
	w.Header().Set("Content-Type", "application/json;charset=utf-8")
	if r.Method != "HEAD" && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		w.Header().Set("Vary", "Accept-Encoding")
		w.Header().Set("Content-Encoding", "gzip")
		w.WriteHeader(status)
		writer := gzip.NewWriter(w)
		_ = json.NewEncoder(writer).Encode(value)
		_ = writer.Close()
		return
	}
	w.WriteHeader(status)
	if r.Method != "HEAD" {
		_ = json.NewEncoder(w).Encode(value)
	}
}
func failure(w http.ResponseWriter, r *http.Request, status int, label, message string) {
	if observer, ok := w.(interface{ ErrorObserved(int, string, string) }); ok {
		observer.ErrorObserved(status, label, message)
	}
	jsonResponse(w, r, status, map[string]any{"code": status, "label": label, "message": message})
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if _, present := r.Header["Request-Id"]; !present {
		var nonce [16]byte
		_, _ = rand.Read(nonce[:])
		nonce[6] = (nonce[6] & 0x0f) | 0x40
		nonce[8] = (nonce[8] & 0x3f) | 0x80
		r = r.Clone(r.Context())
		r.Header.Set("Request-Id", fmt.Sprintf("%x-%x-%x-%x-%x", nonce[:4], nonce[4:6], nonce[6:8], nonce[8:10], nonce[10:]))
	}
	observed := &observedWriter{ResponseWriter: w, logger: s.logger, requestID: r.Header.Get("Request-Id")}
	started := time.Now()
	defer func() {
		if observed.status != 101 {
			status := observed.status
			if status == 0 {
				status = 200
			}
			s.metrics.observe(r.URL.Path, r.Method, status, time.Since(started))
			if status >= 500 {
				s.metrics.errors.Add(1)
			}
		}
	}()
	r = r.WithContext(context.WithValue(r.Context(), requestIDKey{}, r.Header.Get("Request-Id")))
	s.serveHTTP(observed, r)
}
func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	r.Header.Del("X-Wire-API-Version")
	path := r.URL.Path
	version := 0
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) > 0 && looksLikeVersion(parts[0]) {
		v := parts[0]
		if len(parts) > 1 && (parts[1] == "i" || parts[1] == "api-internal") {
			failure(w, r, 404, "unsupported-version", "Internal APIs (`/i/...`) are not under version control")
			return
		}
		n, err := strconv.Atoi(v[1:])
		if err != nil || n < 0 || n > 19 || s.disabled(n) {
			failure(w, r, 404, "unsupported-version", "Version "+v+" is not supported")
			return
		}
		version = n
		path = "/" + strings.Join(parts[1:], "/")
	}
	switch {
	case path == "/i/metrics" && r.Method == "GET":
		s.metrics.serve(w)
		s.mu.Lock()
		count := len(s.sockets)
		s.mu.Unlock()
		fmt.Fprintf(w, "# HELP net_websocket_clients Number of connected websocket clients\n# TYPE net_websocket_clients gauge\nnet_websocket_clients %d.0\n", count)
	case path == "/i/status" && (r.Method == "GET" || r.Method == "HEAD"):
		plain(w, r, 200, "")
	case (path == "/await" || path == "/websocket" || path == "/events" && version >= 8):
		user, ok := r.Header["Z-User"]
		if !ok || len(user) == 0 {
			plain(w, r, 400, "Header Z-User is required")
			return
		}
		if !isUUID(user[0]) {
			plain(w, r, 400, "Error parsing header Z-User failed: invalid UUID")
			return
		}
		conn, hasConn := r.Header["Z-Connection"]
		if path != "/events" && (!hasConn || len(conn) == 0) {
			plain(w, r, 400, "Header Z-Connection is required")
			return
		}
		var clientID *string
		if values, ok := r.URL.Query()["client"]; ok && len(values) > 0 {
			n, err := strconv.ParseUint(values[0], 16, 64)
			if err != nil {
				plain(w, r, 400, "Error parsing query parameter client failed: Failed reading: takeWhile1")
				return
			}
			id := strconv.FormatUint(n, 16)
			clientID = &id
		}
		if !websocket.IsWebSocketUpgrade(r) {
			plain(w, r, 426, "")
			return
		}
		if path == "/events" {
			var marker *string
			if version >= 9 {
				if values, ok := r.URL.Query()["sync_marker"]; ok && len(values) > 0 {
					marker = &values[0]
				}
			}
			s.events(w, r, strings.ToLower(user[0]), clientID, marker)
			return
		} // expanded in the event implementation slice
		s.classic(w, r, socketKey{strings.ToLower(user[0]), conn[0]}, clientID)
	case strings.HasPrefix(path, "/i/push/") && r.Method == "POST":
		ids := strings.Split(strings.TrimPrefix(path, "/i/push/"), "/")
		if len(ids) != 2 {
			plain(w, r, 404, "")
			return
		}
		if !isUUID(ids[0]) {
			plain(w, r, 400, "invalid UUID")
			return
		}
		if !jsonType(r) {
			plain(w, r, 415, "")
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			failure(w, r, 500, "server-error", "Server Error")
			return
		}
		if s.push(socketKey{strings.ToLower(ids[0]), ids[1]}, body) {
			plain(w, r, 200, "")
		} else {
			failure(w, r, 410, "general", "client gone")
		}
	case path == "/i/bulkpush" && r.Method == "POST":
		s.bulk(w, r)
	case strings.HasPrefix(path, "/i/presences/") && r.Method == "HEAD":
		ids := strings.Split(strings.TrimPrefix(path, "/i/presences/"), "/")
		if len(ids) != 2 {
			plain(w, r, 404, "")
			return
		}
		if !isUUID(ids[0]) {
			plain(w, r, 400, "invalid UUID")
			return
		}
		found, err := s.remotePresence(r.Context(), strings.ToLower(ids[0]), ids[1])
		if err != nil {
			failure(w, r, 500, "server-error", "Server Error")
		} else if found {
			plain(w, r, 200, "")
		} else {
			failure(w, r, 404, "not-found", "presence not registered")
		}
	default:
		plain(w, r, 404, "")
	}
}
func jsonType(r *http.Request) bool {
	return strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]) == "application/json"
}
func looksLikeVersion(v string) bool {
	if !strings.HasPrefix(v, "v") {
		return false
	}
	for _, c := range v[1:] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
func (s *Server) disabled(v int) bool {
	for _, x := range s.Config.Disabled {
		if x == "v"+strconv.Itoa(v) || x == "development" && v == 19 {
			return true
		}
	}
	return false
}

var upgrader = websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}

func (s *Server) classic(w http.ResponseWriter, r *http.Request, key socketKey, clientID *string) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c := &liveSocket{conn: conn, done: make(chan struct{})}
	s.mu.Lock()
	s.sockets[key] = c
	s.mu.Unlock()
	defer func() { close(c.done); s.remove(key, c); conn.Close() }()
	if err = s.register(r.Context(), key, clientID); err != nil {
		c.close(1000, "close")
		return
	}
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(30 * time.Second)) })
	conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	conn.SetPingHandler(func(data string) error { return c.write(websocket.PongMessage, []byte(data)) })
	go func() {
		tick := time.NewTicker(15 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-c.done:
				return
			case <-tick.C:
				if c.write(websocket.PingMessage, nil) != nil {
					conn.Close()
					return
				}
			}
		}
	}()
	lifetime := time.AfterFunc(3*24*time.Hour, func() { c.close(1000, "close"); conn.Close() })
	defer lifetime.Stop()
	for {
		kind, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if (kind == websocket.TextMessage || kind == websocket.BinaryMessage) && string(data) == "ping" {
			if c.write(websocket.BinaryMessage, []byte("pong")) != nil {
				return
			}
		}
	}
}
func (s *Server) remove(key socketKey, c *liveSocket) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sockets[key] == c {
		delete(s.sockets, key)
	}
}
func (s *Server) push(key socketKey, body []byte) bool {
	s.mu.Lock()
	c := s.sockets[key]
	s.mu.Unlock()
	if c == nil {
		return false
	}
	for attempt := 0; attempt < 4; attempt++ {
		if c.write(websocket.BinaryMessage, body) == nil {
			return true
		}
		if attempt < 3 {
			time.Sleep(time.Duration(100*(1<<attempt)) * time.Millisecond)
		}
	}
	s.remove(key, c)
	c.close(1000, "close")
	c.conn.Close()
	return false
}
func (s *Server) presenceRequest(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	endpoint := "http://" + s.Config.Gundeck.Address() + path
	for attempt := 0; attempt < 4; attempt++ {
		request, err := http.NewRequestWithContext(ctx, method, endpoint, strings.NewReader(string(body)))
		if err != nil {
			return nil, err
		}
		request.Header.Set("Content-Type", "application/json")
		if id, ok := ctx.Value(requestIDKey{}).(string); ok {
			request.Header.Set("Request-Id", id)
		}
		response, err := s.client.Do(request)
		if err == nil {
			raw, readErr := io.ReadAll(response.Body)
			response.Body.Close()
			if response.StatusCode >= 200 && response.StatusCode < 300 && readErr == nil {
				return raw, nil
			}
			err = fmt.Errorf("presence provider failure")
		}
		if attempt == 3 {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(100*(1<<attempt)) * time.Millisecond):
		}
	}
	return nil, fmt.Errorf("presence provider failure")
}
func (s *Server) register(ctx context.Context, key socketKey, clientID *string) error {
	value := map[string]any{"user_id": key.user, "device_id": key.connection, "resource": fmt.Sprintf("http://%s:%d/i/push/%s/%s", *s.Config.Cannon.ExternalHost, s.Config.Cannon.Port, key.user, key.connection), "client_id": clientID}
	body, _ := json.Marshal(value)
	_, err := s.presenceRequest(ctx, "POST", "/i/presences", body)
	return err
}
func (s *Server) remotePresence(ctx context.Context, user, connection string) (bool, error) {
	raw, err := s.presenceRequest(ctx, "GET", "/i/presences/"+url.PathEscape(user), nil)
	if err != nil {
		return false, err
	}
	var values []struct {
		Connection string `json:"device_id"`
	}
	if err = json.Unmarshal(raw, &values); err != nil {
		return false, err
	}
	for _, v := range values {
		if v.Connection == connection {
			return true, nil
		}
	}
	return false, nil
}
func (s *Server) Drain() {
	s.mu.Lock()
	sockets := make([]*liveSocket, 0, len(s.sockets))
	for _, socket := range s.sockets {
		sockets = append(sockets, socket)
	}
	s.mu.Unlock()
	drainBatches(s.Config, len(sockets), func(i int) { sockets[i].close(1000, "close") })
	if s.pool != nil {
		channels := s.pool.channelsForDrain()
		drainBatches(s.Config, len(channels), func(i int) { _ = channels[i].Close() })
		s.pool.close()
	}
}
func drainBatches(config Config, count int, closeOne func(int)) {
	if count == 0 {
		return
	}
	batches := config.Drain.Grace * 1000 / config.Drain.Interval
	batch := max(config.Drain.MinBatch, uint64(count)/batches)
	if batch == 0 {
		panic("empty drain batch")
	}
	for start := 0; start < count; start += int(batch) {
		limit := min(count, start+int(batch))
		var workers sync.WaitGroup
		for i := start; i < limit; i++ {
			workers.Add(1)
			go func(i int) { defer workers.Done(); closeOne(i) }(i)
		}
		pause := time.NewTimer(time.Duration(config.Drain.Interval) * time.Millisecond)
		workers.Wait()
		<-pause.C
	}
}

func (s *Server) ConnectionState(conn net.Conn, state http.ConnState) {
	s.metrics.ConnState(conn, state)
}
func (s *Server) CollectMetrics(ctx context.Context) { s.metrics.Collect(ctx) }
func (s *Server) Listening(address string)           { s.logger.Listening(address) }

type requestIDKey struct{}
