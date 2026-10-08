package gundeck

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/redis/go-redis/v9"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Server struct {
	Config     Config
	redis      redis.UniversalClient
	additional redis.UniversalClient
	logger     *Logger
}

func NewServer(c Config) (*Server, error) {
	r, err := openRedis(c.Redis, false)
	if err != nil {
		return nil, err
	}
	s := &Server{Config: c, redis: r}
	if c.Additional != nil {
		s.additional, err = openRedis(*c.Additional, true)
		if err != nil {
			_ = r.Close()
			return nil, err
		}
	}
	return s, nil
}
func (s *Server) Close() {
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
	case path == "/i/status" && r.Method == "GET":
		plain(w, 200, "")
	case path == "/i/presences" && r.Method == "POST":
		var p Presence
		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(&p); err != nil {
			plain(w, 400, "Invalid presence JSON")
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
		plain(w, 201, "")
	case strings.HasPrefix(path, "/i/presences/") && r.Method == "DELETE":
		plain(w, 200, "")
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
				plain(w, 400, "invalid UUID")
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
