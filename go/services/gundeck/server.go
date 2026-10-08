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
	Config     Config
	redis      redis.UniversalClient
	additional redis.UniversalClient
	logger     *Logger
	cassandra  *gocql.Session
	http       *http.Client
	aws        aws.Config
	sns        *sns.Client
	tokenMu    sync.Mutex
}

func NewServer(c Config) (*Server, error) {
	r, err := openRedis(c.Redis, false)
	if err != nil {
		return nil, err
	}
	s := &Server{Config: c, redis: r, http: &http.Client{Timeout: 10 * time.Second}}
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s.aws, s.sns, err = openAWS(ctx, c)
	if err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}
func (s *Server) Close() {
	if s.cassandra != nil {
		s.cassandra.Close()
	}
	s.http.CloseIdleConnections()
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
	if v == "" {
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
	path := r.URL.Path
	version := 0
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) > 1 && strings.HasPrefix(parts[0], "v") {
		parsed, err := strconv.Atoi(parts[0][1:])
		if err != nil || parsed < 0 || parsed > 19 {
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
	switch {
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
		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(&p); err != nil {
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
		if err = s.addPresence(r.Context(), p); err != nil {
			failure(w, 500, "server-error", "Server Error")
			return
		}
		w.Header().Set("Location", p.Resource)
		emptyJSON(w, 201)
	case strings.HasPrefix(path, "/i/presences/") && r.Method == "DELETE":
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
	var pushes []json.RawMessage
	if err = json.Unmarshal(raw, &pushes); err != nil {
		plain(w, 400, "Invalid push JSON")
		return
	}
	if len(pushes) > 0 {
		failure(w, 501, "not-implemented", "Push delivery qualification in progress")
		return
	}
	emptyJSON(w, 200)
}
