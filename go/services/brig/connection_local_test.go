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

func TestLocalConnectionAdmissionAndDurableEffects(t *testing.T) {
	ctx := context.Background()
	self, target := gocql.TimeUUID(), gocql.TimeUUID()
	conv := gocql.TimeUUID()
	trace := []string{}
	active := true
	lh := "no_consent"
	targetLH := "disabled"
	sameTeam := false
	atLimit := false
	app := false
	rows := map[gocql.UUID]storedConnection{}
	pushes := []map[string]any{}
	s := connectionLocalService{domain: "example.com", limit: 2, now: func() time.Time { return time.Unix(100, 123000000) }, activated: func(_ context.Context, u gocql.UUID) (bool, error) {
		trace = append(trace, "activated")
		return active, nil
	}, user: func(_ context.Context, u gocql.UUID) (*storedUser, error) {
		kind := int32(0)
		if app {
			kind = 2
		}
		return &storedUser{ID: u, Name: "Synthetic", Type: &kind}, nil
	}, legalhold: func(_ context.Context, u *storedUser, _ string) (string, error) {
		trace = append(trace, "legalhold")
		if u.ID == self {
			return lh, nil
		}
		return targetLH, nil
	}, team: func(context.Context, gocql.UUID, string) (*gocql.UUID, error) {
		trace = append(trace, "team")
		if sameTeam {
			return &conv, nil
		}
		return nil, nil
	}, get: func(_ context.Context, u, _ gocql.UUID, _ string) (*storedConnection, error) {
		r, ok := rows[u]
		if !ok {
			return nil, nil
		}
		return &r, nil
	}, count: func(context.Context, gocql.UUID) (int64, error) {
		trace = append(trace, "count")
		if atLimit {
			return 2, nil
		}
		return 0, nil
	}, insert: func(_ context.Context, r storedConnection) error {
		trace = append(trace, "insert")
		rows[r.from] = r
		return nil
	}, update: func(_ context.Context, r storedConnection) error {
		trace = append(trace, "update")
		rows[r.from] = r
		return nil
	}, createConversation: func(context.Context, gocql.UUID, gocql.UUID, string, string) (gocql.UUID, error) {
		trace = append(trace, "conversation")
		return conv, nil
	}, acceptConversation: func(context.Context, gocql.UUID, gocql.UUID, string, string) (int, error) {
		trace = append(trace, "accept")
		return 2, nil
	}, notify: func(_ context.Context, b []byte, _ string) {
		trace = append(trace, "notify")
		var p []map[string]any
		if json.Unmarshal(b, &p) != nil {
			t.Fatal("invalid push")
		}
		pushes = append(pushes, p[0])
	}}
	assertDenied := func(want error) {
		t.Helper()
		_, _, err := s.create(ctx, self, target, "synthetic", "req")
		if !errors.Is(err, want) || len(rows) != 0 || len(pushes) != 0 {
			t.Fatal("admission allowed effects", err, trace)
		}
		trace = nil
	}
	active = false
	assertDenied(errConnectionNoIdentity)
	active = true
	targetLH = "enabled"
	assertDenied(errPrekeyConsent)
	targetLH = "disabled"
	sameTeam = true
	assertDenied(errConnectionSameTeam)
	sameTeam = false
	app = true
	assertDenied(errConnectionInvalidUser)
	app = false
	atLimit = true
	assertDenied(errConnectionLimit)
	atLimit = false
	row, created, err := s.create(ctx, self, target, "synthetic", "req")
	if err != nil || !created || row.relation != 4 || rows[target].relation != 2 || len(pushes) != 2 {
		t.Fatal("creation state/effects differ", err, trace)
	}
	if rows[self].conversation == nil || *rows[self].conversation != *rows[target].conversation || trace[len(trace)-4] != "insert" || trace[len(trace)-3] != "insert" {
		t.Fatal("notifications preceded durable rows", trace)
	}
	if pushes[0]["origin"] != self.String() || pushes[0]["origin_connection"] != "synthetic" {
		t.Fatal("push origin differs")
	}
	// Reciprocal create accepts both rows and must not hit the limit for
	// an already sent/accepted actor. This preserves native branch ordering.
	reverse := rows[target]
	reverse.relation = 4
	rows[target] = reverse
	atLimit = true
	trace = nil
	row, created, err = s.create(ctx, self, target, "synthetic", "req")
	if err != nil || created || row.relation != 0 || rows[target].relation != 0 {
		t.Fatal("reciprocal create differs", err, trace)
	}
	for _, call := range trace {
		if call == "count" || call == "conversation" {
			t.Fatal("accepted/sent path checked limit or created conversation")
		}
	}
	rows[self] = storedConnection{from: self, to: target, relation: 1}
	if _, _, err = s.create(ctx, self, target, "synthetic", "req"); !errors.Is(err, errConnectionTransition) {
		t.Fatal("blocked actor allowed reconnect")
	}
}
