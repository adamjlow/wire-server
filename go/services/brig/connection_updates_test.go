// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/apache/cassandra-gocql-driver/v2"
	"testing"
	"time"
)

func TestLocalConnectionUpdateEffectsAndHistory(t *testing.T) {
	self, target, conv := gocql.TimeUUID(), gocql.TimeUUID(), gocql.TimeUUID()
	rows := map[gocql.UUID]storedConnection{}
	trace := []string{}
	pushes := 0
	local := connectionLocalService{domain: "example.com", now: func() time.Time { return time.Unix(10, 0) }, limit: 10, get: func(_ context.Context, u, _ gocql.UUID, _ string) (*storedConnection, error) {
		r, ok := rows[u]
		if !ok {
			return nil, nil
		}
		return &r, nil
	}, count: func(context.Context, gocql.UUID) (int64, error) { trace = append(trace, "limit"); return 0, nil }, user: func(context.Context, gocql.UUID) (*storedUser, error) { return &storedUser{Name: "Synthetic"}, nil }, update: func(_ context.Context, r storedConnection) error {
		trace = append(trace, "write")
		rows[r.from] = r
		return nil
	}, notify: func(context.Context, []byte, string) { trace = append(trace, "notify"); pushes++ }}
	service := connectionUpdateService{local: local, mls: true, accept: func(context.Context, gocql.UUID, gocql.UUID, *string, string) (int, error) {
		trace = append(trace, "accept")
		return 2, nil
	}, block: func(context.Context, gocql.UUID, gocql.UUID, string, string) error {
		trace = append(trace, "block")
		return nil
	}, unblock: func(context.Context, gocql.UUID, gocql.UUID, string, *string, string) (int, error) {
		trace = append(trace, "unblock")
		return 2, nil
	}, established: func(context.Context, gocql.UUID, gocql.UUID, string, string) (int, error) {
		trace = append(trace, "established")
		return 1, nil
	}}
	reset := func(a, b int32) {
		rows[self] = storedConnection{from: self, to: target, relation: a, domain: local.domain, conversation: &conv, conversationDomain: local.domain}
		rows[target] = storedConnection{from: target, to: self, relation: b, domain: local.domain, conversation: &conv, conversationDomain: local.domain}
		trace = nil
		pushes = 0
	}
	ctx := context.Background()
	reset(6, 4)
	if _, err := service.apply(ctx, self, target, 6, nil, "r"); !errors.Is(err, errConnectionTransition) || len(trace) != 0 {
		t.Fatal("history protection changed")
	}
	reset(0, 0)
	if row, err := service.apply(ctx, self, target, 0, nil, "r"); err != nil || row != nil || len(trace) != 0 {
		t.Fatal("unchanged update had effects")
	}
	reset(0, 0)
	if _, err := service.apply(ctx, self, target, 1, nil, "r"); err != nil {
		t.Fatal(err)
	}
	if len(trace) != 5 || trace[0] != "block" || trace[1] != "established" || trace[2] != "block" || trace[3] != "write" || trace[4] != "notify" {
		t.Fatal("block/MLS/durable ordering differs", trace)
	}
	reset(1, 4)
	if row, err := service.apply(ctx, self, target, 4, nil, "r"); err != nil || row.relation != 0 || rows[target].relation != 0 || pushes != 2 {
		t.Fatal("unblock Sent counterpart behavior differs", err, trace)
	}
	reset(4, 2)
	if row, err := service.apply(ctx, self, target, 5, nil, "r"); err != nil || row.relation != 5 || rows[target].relation != 5 || pushes != 2 {
		t.Fatal("cancel pair behavior differs", err, trace)
	}
	reset(4, 4)
	if row, err := service.apply(ctx, self, target, 0, nil, "r"); err != nil || row.from != target || rows[self].relation != 0 || pushes != 1 {
		t.Fatal("native Sent/Sent returned reverse row lost", err)
	}
	reset(0, 0)
	service.block = func(context.Context, gocql.UUID, gocql.UUID, string, string) error { return errors.New("peer failure") }
	if _, err := service.apply(ctx, self, target, 1, nil, "r"); err == nil || rows[self].relation != 0 || pushes != 0 {
		t.Fatal("peer failure changed durable state")
	}
}
func TestConnectionOriginAbsentAndEmpty(t *testing.T) {
	var push []map[string]any
	s := connectionLocalService{notify: func(_ context.Context, b []byte, _ string) {
		if err := json.Unmarshal(b, &push); err != nil {
			t.Fatal(err)
		}
	}}
	row := storedConnection{from: gocql.TimeUUID(), to: gocql.TimeUUID(), domain: "example.com"}
	if err := s.event(context.Background(), row.from, nil, "r", row, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := push[0]["origin_connection"]; ok {
		t.Fatal("absent origin connection emitted")
	}
	empty := ""
	if err := s.event(context.Background(), row.from, &empty, "r", row, nil); err != nil {
		t.Fatal(err)
	}
	if value, ok := push[0]["origin_connection"]; !ok || value != "" {
		t.Fatal("present empty connection lost")
	}
}
