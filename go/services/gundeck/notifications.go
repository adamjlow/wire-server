package gundeck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

const notificationColumns = "SELECT id, payload, payload_ref, payload_ref_size, clients FROM notifications "

type Notification struct {
	ID      string          `json:"id"`
	Payload json.RawMessage `json:"payload"`
}
type notificationRow struct {
	id            gocql.UUID
	payload       *[]byte
	reference     *gocql.UUID
	referenceSize *int32
	clients       []string
}

func openCassandra(c Config) (*gocql.Session, error) {
	cluster := gocql.NewCluster(c.Cassandra.Endpoint.Host)
	cluster.Port = int(c.Cassandra.Endpoint.Port)
	cluster.Keyspace = c.Cassandra.Keyspace
	cluster.Consistency = gocql.LocalQuorum
	cluster.Timeout = 10 * time.Second
	cluster.ConnectTimeout = 10 * time.Second
	if c.Cassandra.Datacentre != "" {
		cluster.HostFilter = gocql.DataCentreHostFilter(c.Cassandra.Datacentre)
	}
	if c.Cassandra.TLSCA != "" {
		cfg, err := verifiedTLS(c.Cassandra.Endpoint.Host, c.Cassandra.TLSCA)
		if err != nil {
			return nil, err
		}
		cluster.SslOpts = &gocql.SslOptions{Config: cfg, EnableHostVerification: true}
	}
	session, err := cluster.CreateSession()
	if err != nil {
		return nil, fmt.Errorf("Cassandra initialization failed")
	}
	return session, nil
}
func (s *Server) query(ctx context.Context, statement string, args ...any) *gocql.Query {
	return s.cassandra.Query(statement, args...).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.ExponentialBackoffRetryPolicy{NumRetries: 1, Min: 100 * time.Millisecond, Max: 1600 * time.Millisecond})
}
func (s *Server) notificationPage(ctx context.Context, statement string, args []any, state []byte, size int) ([]notificationRow, []byte, error) {
	iter := s.query(ctx, statement, args...).PageSize(size).PageState(state).Iter()
	rows := []notificationRow{}
	for {
		var row notificationRow
		if !iter.Scan(&row.id, &row.payload, &row.reference, &row.referenceSize, &row.clients) {
			break
		}
		rows = append(rows, row)
	}
	next := append([]byte(nil), iter.PageState()...)
	return rows, next, iter.Close()
}
func (row notificationRow) size() int32 {
	if row.payload != nil {
		return int32(len(*row.payload))
	}
	if row.referenceSize != nil {
		return *row.referenceSize
	}
	return 0
}
func validPayload(payload []byte) bool {
	var values []json.RawMessage
	if json.Unmarshal(payload, &values) != nil || len(values) == 0 {
		return false
	}
	for _, value := range values {
		var object map[string]json.RawMessage
		if json.Unmarshal(value, &object) != nil || object == nil {
			return false
		}
	}
	return true
}
func (s *Server) loadNotification(ctx context.Context, row notificationRow, client *string) (*Notification, error) {
	// The Haskell implementation resolves payload references before filtering clients.
	var payload []byte
	if row.payload != nil {
		payload = *row.payload
	} else if row.reference != nil {
		err := s.query(ctx, "SELECT payload FROM notification_payload WHERE id = ?", *row.reference).Scan(&payload)
		if errors.Is(err, gocql.ErrNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
	} else {
		return nil, nil
	}
	if client != nil && len(row.clients) > 0 {
		found := false
		for _, c := range row.clients {
			if c == *client {
				found = true
				break
			}
		}
		if !found {
			return nil, nil
		}
	}
	if !validPayload(payload) {
		return nil, nil
	}
	return &Notification{ID: row.id.String(), Payload: json.RawMessage(payload)}, nil
}
func (s *Server) fetchNotification(ctx context.Context, user, id string, client *string) (*Notification, error) {
	uid, _ := gocql.ParseUUID(user)
	nid, _ := gocql.ParseUUID(id)
	rows, _, err := s.notificationPage(ctx, notificationColumns+"WHERE user = ? AND id = ?", []any{uid, nid}, nil, 1)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return s.loadNotification(ctx, rows[0], client)
}
func (s *Server) fetchLastNotification(ctx context.Context, user string, client *string) (*Notification, error) {
	uid, _ := gocql.ParseUUID(user)
	rows, _, err := s.notificationPage(ctx, notificationColumns+"WHERE user = ? ORDER BY id DESC LIMIT 1", []any{uid}, nil, 1)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	first := rows[0].id
	n, err := s.loadNotification(ctx, rows[0], client)
	if err != nil || n != nil {
		return n, err
	}
	var state []byte
	for {
		rows, next, err := s.notificationPage(ctx, notificationColumns+"WHERE user = ? AND id < ? ORDER BY id DESC", []any{uid, first}, state, s.pageSize())
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			n, err := s.loadNotification(ctx, row, client)
			if err != nil || n != nil {
				return n, err
			}
		}
		if len(next) == 0 {
			return nil, nil
		}
		state = next
	}
}
func (s *Server) pageSize() int {
	if s.Config.Settings.PageSize != nil {
		return int(*s.Config.Settings.PageSize)
	}
	return 100
}
func (s *Server) payloadLimit() int32 {
	if s.Config.Settings.PayloadLimit != nil {
		return *s.Config.Settings.PayloadLimit
	}
	return 5 * 1024 * 1024
}

// truncatePayloadRows mirrors the per-Cassandra-page byte boundary. Skipped client
// rows still consume the budget; the first row in each page is always eligible.
func truncatePayloadRows(rows []notificationRow, remaining int32) ([]notificationRow, int32, bool) {
	for i, row := range rows {
		if i > 0 && remaining <= 0 {
			return rows[:i], remaining, i+1 < len(rows)
		}
		remaining -= row.size()
	}
	return rows, remaining, false
}
func (s *Server) fetchNotifications(ctx context.Context, user string, client *string, since string, size int) ([]Notification, bool, bool, error) {
	uid, _ := gocql.ParseUUID(user)
	statement := notificationColumns + "WHERE user = ? ORDER BY id ASC"
	args := []any{uid}
	gap := false
	if since != "" {
		nid, _ := gocql.ParseUUID(since)
		var found gocql.UUID
		err := s.query(ctx, "SELECT id FROM notifications WHERE user = ? AND id = ?", uid, nid).Scan(&found)
		if err != nil && !errors.Is(err, gocql.ErrNotFound) {
			return nil, false, false, err
		}
		gap = errors.Is(err, gocql.ErrNotFound)
		statement = notificationColumns + "WHERE user = ? AND id > ? ORDER BY id ASC"
		args = append(args, nid)
	}
	notifications := []Notification{}
	remaining := s.payloadLimit()
	more := true
	var state []byte
	for len(notifications) < size+1 && remaining > 0 && more {
		rows, next, err := s.notificationPage(ctx, statement, args, state, s.pageSize())
		if err != nil {
			return nil, false, false, err
		}
		var truncated bool
		rows, remaining, truncated = truncatePayloadRows(rows, remaining)
		for _, row := range rows {
			n, err := s.loadNotification(ctx, row, client)
			if err != nil {
				return nil, false, false, err
			}
			if n != nil {
				notifications = append(notifications, *n)
			}
		}
		more = len(next) > 0 || truncated
		state = next
		// A truncated page always exhausted its budget. Do not repeat a terminal page.
		if remaining <= 0 || len(next) == 0 {
			break
		}
	}
	if len(notifications) > size {
		notifications = notifications[:size]
		more = true
	}
	return notifications, more, gap, nil
}
func queryClient(w http.ResponseWriter, r *http.Request) (*string, bool) {
	value, ok := r.URL.Query()["client"]
	if !ok {
		return nil, true
	}
	if len(value) == 0 || !clientValid(value[0]) {
		plain(w, 400, "Error parsing query parameter client failed: Failed reading: takeWhile1")
		return nil, false
	}
	client := strings.ToLower(value[0])
	return &client, true
}
func (s *Server) notificationHTTP(w http.ResponseWriter, r *http.Request, path string, version int) {
	user, ok := principal(w, r)
	if !ok {
		return
	}
	id := strings.TrimPrefix(path, "/notifications/")
	if path != "/notifications" && id != "last" && !uuidValid(id) {
		plain(w, 400, "invalid UUID")
		return
	}
	client, ok := queryClient(w, r)
	if !ok {
		return
	}
	if path != "/notifications" {
		var n *Notification
		var err error
		if id == "last" {
			n, err = s.fetchLastNotification(r.Context(), user, client)
		} else {
			n, err = s.fetchNotification(r.Context(), user, id, client)
		}
		if err != nil {
			failure(w, 500, "server-error", "Server Error")
			return
		}
		if n == nil {
			notificationNotFound(w)
			return
		}
		jsonResponse(w, 200, n)
		return
	}
	size := 1000
	if value, exists := r.URL.Query()["size"]; exists {
		parsed, err := strconv.ParseInt(value[0], 10, 32)
		if err != nil {
			plain(w, 400, "Error parsing query parameter size failed: Failed reading: takeWhile1")
			return
		}
		if parsed < 100 || parsed > 10000 {
			plain(w, 400, "Error parsing query parameter size failed: outside range [100, 10000]")
			return
		}
		size = int(parsed)
	}
	since := ""
	invalidLegacy := false
	if value, exists := r.URL.Query()["since"]; exists {
		since = strings.ToLower(value[0])
		if version <= 2 {
			if !uuidValid(since) || since[14] != '1' {
				since = ""
				invalidLegacy = true
			}
		} else {
			if !uuidValid(since) {
				plain(w, 400, "Error parsing query parameter since failed: invalid UUID")
				return
			}
			if since[14] != '1' {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(400)
				_ = json.NewEncoder(w).Encode(map[string]any{"code": 400, "label": "bad-request", "message": "Invalid Notification ID"})
				return
			}
		}
	}
	if client != nil {
		request, err := http.NewRequestWithContext(r.Context(), http.MethodPost, "http://"+s.Config.Brig.Address()+"/i/clients/"+user+"/"+*client+"/activity", nil)
		if err != nil {
			failure(w, 500, "server-error", "Server Error")
			return
		}
		response, err := s.http.Do(request)
		if err != nil {
			failure(w, 500, "server-error", "Server Error")
			return
		}
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
	}
	notifications, more, gap, err := s.fetchNotifications(r.Context(), user, client, since, size)
	if err != nil {
		failure(w, 500, "server-error", "Server Error")
		return
	}
	if gap && version >= 3 {
		notificationNotFound(w)
		return
	}
	status := 200
	if gap || invalidLegacy {
		status = 404
	}
	jsonResponse(w, status, map[string]any{"notifications": notifications, "has_more": more, "time": time.Now().UTC().Format("2006-01-02T15:04:05Z")})
}
func notificationNotFound(w http.ResponseWriter) {
	failure(w, 404, "not-found", "Some notifications not found")
}
