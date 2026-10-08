package gundeck

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/redis/go-redis/v9"
	"net/url"
	"os"
	"strings"
	"time"
)

type Presence struct {
	User     string  `json:"user_id"`
	Device   string  `json:"device_id"`
	Resource string  `json:"resource"`
	Client   *string `json:"client_id"`
	Created  int64   `json:"created_at"`
	field    string
}
type presenceData struct {
	Resource string  `json:"r"`
	Client   *string `json:"c"`
	Created  int64   `json:"t"`
}

func openRedis(c RedisEndpoint, additional bool) (redis.UniversalClient, error) {
	userKey, passwordKey := "REDIS_USERNAME", "REDIS_PASSWORD"
	if additional {
		userKey, passwordKey = "REDIS_ADDITIONAL_WRITE_USERNAME", "REDIS_ADDITIONAL_WRITE_PASSWORD"
	}
	options := &redis.UniversalOptions{Addrs: []string{c.Address()}, Username: os.Getenv(userKey), Password: os.Getenv(passwordKey), DialTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, PoolSize: 32, MaxRetries: 3, Protocol: 2}
	if c.EnableTLS {
		var err error
		options.TLSConfig, err = verifiedTLS(c.Host, c.CA)
		if err != nil {
			return nil, err
		}
	}
	if c.Mode == "cluster" {
		if options.TLSConfig != nil {
			// tls.DialWithDialer infers the actual redirected node hostname.
			options.TLSConfig.ServerName = ""
		}
		return redis.NewClusterClient(options.Cluster()), nil
	}
	if c.Mode != "master" {
		return nil, fmt.Errorf("invalid Redis connection mode")
	}
	return redis.NewClient(options.Simple()), nil
}
func decodePresence(user, field, raw string) (Presence, bool) {
	var data presenceData
	if strings.HasPrefix(raw, "http") {
		data.Resource = raw
	} else if json.Unmarshal([]byte(raw), &data) != nil {
		return Presence{}, false
	}
	parsed, err := url.Parse(data.Resource)
	if err != nil || parsed.Scheme == "" {
		return Presence{}, false
	}
	return Presence{User: user, Device: strings.SplitN(field, "@", 2)[0], Resource: data.Resource, Client: data.Client, Created: data.Created, field: field}, true
}
func (s *Server) listPresence(ctx context.Context, user string) ([]Presence, error) {
	values, err := s.redis.HGetAll(ctx, "user:"+user).Result()
	if err != nil {
		return nil, err
	}
	out := make([]Presence, 0, len(values))
	for field, raw := range values {
		if p, ok := decodePresence(user, field, raw); ok {
			out = append(out, p)
		}
	}
	return out, nil
}
func (s *Server) addPresence(ctx context.Context, p Presence) error {
	raw, err := json.Marshal(presenceData{Resource: p.Resource, Client: p.Client, Created: time.Now().UnixMilli()})
	if err != nil {
		return err
	}
	for _, client := range []redis.UniversalClient{s.redis, s.additional} {
		if client == nil {
			continue
		}
		_, err = client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.HSet(ctx, "user:"+p.User, p.Device, string(raw))
			pipe.Expire(ctx, "user:"+p.User, 7*24*time.Hour)
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// Compare the original stored generation before deleting: an old delivery result
// must never delete a newly registered socket, including legacy Redis fields.
func (s *Server) removePresence(ctx context.Context, p Presence) error {
	for _, client := range []redis.UniversalClient{s.redis, s.additional} {
		if client == nil {
			continue
		}
		err := client.Watch(ctx, func(tx *redis.Tx) error {
			raw, err := tx.HGet(ctx, "user:"+p.User, p.field).Result()
			if err == redis.Nil {
				return nil
			}
			if err != nil {
				return err
			}
			current, ok := decodePresence(p.User, p.field, raw)
			if !ok || current.Resource != p.Resource || current.Created != p.Created || !sameClient(current.Client, p.Client) {
				return nil
			}
			_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error { pipe.HDel(ctx, "user:"+p.User, p.field); return nil })
			return err
		}, "user:"+p.User)
		if err != nil {
			return err
		}
	}
	return nil
}
func sameClient(a, b *string) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }
