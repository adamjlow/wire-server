// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func TestDeletionEventsOrderingAndWireContract(t *testing.T) {
	for _, failQueue := range []bool{false, true} {
		t.Run(map[bool]string{false: "rescind-best-effort", true: "persistent-failure"}[failQueue], func(t *testing.T) {
			user, accepted, pending, history, remote := gocql.TimeUUID(), gocql.TimeUUID(), gocql.TimeUUID(), gocql.TimeUUID(), gocql.TimeUUID()
			conv := gocql.TimeUUID()
			at := time.Date(2026, 10, 11, 1, 2, 3, 123456789, time.UTC)
			var calls []string
			localPages, remotePages := 0, 0
			s := accountDeletionEvents{domain: "example.com", now: func() time.Time { return at },
				page: func(_ context.Context, u gocql.UUID, isRemote bool, start *gocql.UUID, size int, state []byte) (connectionPage, error) {
					if u != user {
						t.Error("wrong source")
					}
					if !isRemote {
						localPages++
						calls = append(calls, "local-page")
						if size != 501 || len(state) != 0 {
							t.Error("wrong native local paging")
						}
						if localPages == 1 {
							if start != nil {
								t.Error("unexpected first cursor")
							}
							return connectionPage{rows: []storedConnection{{from: user, to: accepted, relation: 0}, {from: user, to: pending, relation: 4, conversation: &conv, conversationDomain: "example.com"}, {from: user, to: history, relation: 10}}, state: []byte{1}}, nil
						}
						if start == nil || *start != history {
							t.Error("wrong maximum local cursor")
						}
						return connectionPage{}, nil
					}
					remotePages++
					calls = append(calls, "remote-page")
					if size != 1000 || start != nil {
						t.Error("wrong native remote paging")
					}
					if remotePages == 1 {
						if len(state) != 0 {
							t.Error("unexpected remote first state")
						}
						return connectionPage{rows: []storedConnection{{to: remote, domain: "z.example.com", relation: 4}, {to: history, domain: "a.example.com", relation: 10}}, state: []byte{7, 8}}, nil
					}
					if !reflect.DeepEqual(state, []byte{7, 8}) {
						t.Error("opaque remote state changed")
					}
					return connectionPage{}, nil
				},
				notify: func(_ context.Context, raw []byte, rid string) {
					calls = append(calls, "direct")
					if rid != "synthetic-delete" {
						t.Error("missing request ID")
					}
					var pushes []struct {
						Origin     string
						Recipients []struct {
							User    string `json:"user_id"`
							Route   string
							Clients []string
						}
						Payload []struct {
							Type, ID  string
							Qualified struct{ ID, Domain string } `json:"qualified_id"`
						}
						Connection *string `json:"origin_connection"`
					}
					if json.Unmarshal(raw, &pushes) != nil || len(pushes) != 1 {
						t.Fatal("invalid push")
					}
					p := pushes[0]
					if p.Origin != user.String() || p.Connection != nil || len(p.Recipients) != 1 || len(p.Payload) != 1 || p.Payload[0].Type != "user.delete" || p.Payload[0].ID != user.String() || p.Payload[0].Qualified.Domain != "example.com" {
						t.Error("wrong user-delete wire event")
					}
					if p.Recipients[0].Route != "direct" || p.Recipients[0].Clients == nil || p.Recipients[0].User != user.String() && p.Recipients[0].User != accepted.String() {
						t.Error("wrong deletion recipient")
					}
				},
				connectionEvent: func(_ context.Context, origin gocql.UUID, connection *string, rid string, row storedConnection, name *string) error {
					calls = append(calls, "cancelled")
					if origin != user || connection != nil || name != nil || row.from != pending || row.to != user || row.relation != 5 || row.domain != "example.com" || row.conversation == nil || *row.conversation != conv || !row.updated.Equal(at.Truncate(time.Millisecond)) {
						t.Error("wrong reversed cancellation")
					}
					return nil
				},
				enqueue: func(_ context.Context, domain string, raw []byte) error {
					calls = append(calls, "queue:"+domain)
					var bundle struct {
						Notifications []struct {
							Own        string `json:"ownDomain"`
							Component  string `json:"targetComponent"`
							Path, Body string
							Versions   map[string]int `json:"bodyVersions"`
							Request    string         `json:"requestId"`
						}
						Policy string `json:"unsupportedVersionPolicy"`
					}
					if json.Unmarshal(raw, &bundle) != nil || len(bundle.Notifications) != 1 || bundle.Policy != "keep_queued" {
						t.Fatal("invalid bundle")
					}
					n := bundle.Notifications[0]
					var body struct {
						User        string
						Connections []string
					}
					if n.Own != "example.com" || n.Component != "brig" || n.Path != "on-user-deleted-connections" || n.Request != "synthetic-delete" || !reflect.DeepEqual(n.Versions, map[string]int{"from": 0}) || json.Unmarshal([]byte(n.Body), &body) != nil || body.User != user.String() || len(body.Connections) != 1 {
						t.Error("wrong persistent bundle wire contract")
					}
					if failQueue {
						return errors.New("synthetic queue failure")
					}
					return nil
				},
				rescind: func(_ context.Context, domain string, from, to gocql.UUID, rid string) error {
					calls = append(calls, "rescind")
					if domain != "z.example.com" || from != user || to != remote {
						t.Error("wrong remote rescind")
					}
					return errors.New("synthetic unavailable remote")
				},
			}
			err := s.generate(context.Background(), user, "synthetic-delete")
			want := []string{"direct", "local-page", "direct", "cancelled", "local-page", "remote-page", "queue:a.example.com"}
			if !failQueue {
				want = append(want, "queue:z.example.com", "rescind", "remote-page")
			}
			if (err != nil) != failQueue || !reflect.DeepEqual(calls, want) {
				t.Fatalf("error=%v calls=%v", err, calls)
			}
		})
	}
}

func TestDeletionNotificationBounds(t *testing.T) {
	for _, size := range []int{0, 1001} {
		if _, err := deletionNotificationBundle("example.com", gocql.UUID{}, make([]storedConnection, size), "synthetic"); err == nil {
			t.Fatal("invalid bundle size accepted")
		}
	}
	calls := 0
	s := accountDeletionEvents{domain: "example.com", notify: func(context.Context, []byte, string) { calls++ }}
	if s.generate(context.Background(), gocql.UUID{}, "synthetic") == nil || calls != 0 {
		t.Fatal("missing dependency produced effects")
	}
	recipients := make([]gocql.UUID, 129)
	for i := range recipients {
		recipients[i] = gocql.TimeUUID()
	}
	if s.direct(context.Background(), gocql.UUID{}, recipients, "synthetic") != nil || calls != 2 {
		t.Fatal("native128 recipient chunking changed")
	}
}
