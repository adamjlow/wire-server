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

func TestRemoteConnectionReactionAndAdmissionOrder(t *testing.T) {
	ctx := context.Background()
	self, target := gocql.TimeUUID(), gocql.TimeUUID()
	trace := []string{}
	allowed := true
	peerFail := false
	var saved *storedConnection
	local := connectionLocalService{domain: "a.example", limit: 1, now: time.Now, activated: func(context.Context, gocql.UUID) (bool, error) { return true, nil }, user: func(context.Context, gocql.UUID) (*storedUser, error) { return &storedUser{ID: self}, nil }, get: func(context.Context, gocql.UUID, gocql.UUID, string) (*storedConnection, error) { return saved, nil }, count: func(context.Context, gocql.UUID) (int64, error) { trace = append(trace, "limit"); return 0, nil }, insert: func(_ context.Context, row storedConnection) error {
		trace = append(trace, "insert")
		saved = &row
		return nil
	}, update: func(_ context.Context, row storedConnection) error {
		trace = append(trace, "update")
		saved = &row
		return nil
	}, notify: func(context.Context, []byte, string) { trace = append(trace, "notify") }}
	s := connectionRemoteService{local: local, allowed: func(context.Context, string, *gocql.UUID) (bool, error) {
		trace = append(trace, "policy")
		return allowed, nil
	}, call: func(_ context.Context, domain, path, rid string, input any) (json.RawMessage, error) {
		trace = append(trace, path)
		if peerFail {
			return nil, errors.New("peer unavailable")
		}
		if path == "get-users-by-ids" {
			raw, err := json.Marshal([]publicUserProfile{{QualifiedID: qualifiedUserID{target.String(), "b.example"}, Name: "Synthetic", Legalhold: "no_consent", Type: "regular"}})
			return raw, err
		}
		return json.RawMessage(`{"tag":"NewConnectionResponseOk","contents":"RemoteConnect"}`), nil
	}, upsert: func(_ context.Context, _, _ gocql.UUID, _ string, _ gocql.UUID, _ string, remote, included bool, _ string) error {
		trace = append(trace, "upsert")
		if remote || !included {
			t.Fatal("reaction membership differs")
		}
		return nil
	}}
	allowed = false
	if _, _, err := s.create(ctx, self, target, "b.example", nil, "r"); !errors.Is(err, errConnectionNotFederating) || saved != nil || len(trace) != 2 {
		t.Fatal("federation rejection caused effects", trace)
	}
	allowed = true
	trace = nil
	peerFail = true
	if _, _, err := s.create(ctx, self, target, "b.example", nil, "r"); err == nil || saved != nil {
		t.Fatal("peer failure caused creation")
	}
	peerFail = false
	trace = nil
	row, created, err := s.create(ctx, self, target, "b.example", nil, "r")
	if err != nil || !created || row.relation != 0 {
		t.Fatal("remote reciprocal reaction differs", err)
	}
	want := []string{"get-users-by-ids", "policy", "limit", "send-connection-action", "upsert", "insert", "notify"}
	if len(trace) != len(want) {
		t.Fatal(trace)
	}
	for i, step := range want {
		if trace[i] != step {
			t.Fatal("remote effect order differs", trace)
		}
	}
	// Incoming admission must run before any storage/activation or Galley effect.
	allowed = false
	trace = nil
	response, err := s.incoming(ctx, self, target, "b.example", nil, "connect", "r")
	if err != nil || response["tag"] != "NewConnectionResponseNotFederating" || len(trace) != 1 {
		t.Fatal("incoming policy order differs")
	}
	if _, changed := remoteConnectionTransition(1, "connect", true); changed {
		t.Fatal("remote peer can bypass blocked local state")
	}
	id, domain := connectionOne2One(self, target, "a.example", "b.example", false)
	other, owner := connectionOne2One(target, self, "b.example", "a.example", false)
	if id != other || domain != owner || id[6]>>4 != 5 || id[8]>>6 != 2 {
		t.Fatal("native deterministic UUID symmetry/version differs")
	}
}
