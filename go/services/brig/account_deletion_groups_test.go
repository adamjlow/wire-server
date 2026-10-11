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

func TestNativeTeamMemberDecode(t *testing.T) {
	id := gocql.TimeUUID().String()
	for _, scenario := range []struct {
		body  string
		valid bool
	}{
		{`{"user":"` + id + `","permissions":{"self":8191,"copy":0}}`, true},
		{`{"user":"` + id + `","permissions":{"self":0,"copy":8192},"legalhold_status":"enabled"}`, true},
		{`{"user":"` + id + `","permissions":{"self":8191,"copy":1},"created_by":"` + id + `","created_at":"2026-10-11T12:00:00.123Z"}`, true},
		{`{"user":"` + id + `","permissions":{"self":0,"copy":1}}`, false},
		{`{"user":"` + id + `","permissions":{"self":0,"copy":0},"created_by":"` + id + `"}`, false},
		{`{"user":"` + id + `","permissions":{"self":0,"copy":0},"legalhold_status":"bad"}`, false},
		{`{"legalhold_status":"enabled"}`, false},
		{`{"User":"` + id + `","permissions":{"self":0,"copy":0}}`, false},
		{`{"user":"` + id + `","permissions":{"Self":0,"copy":0}}`, false},
		{`null`, false},
	} {
		_, err := decodeTeamMember([]byte(scenario.body))
		if (err == nil) != scenario.valid {
			t.Fatalf("valid=%v error=%v", scenario.valid, err)
		}
	}
	for _, scenario := range []struct {
		raw   string
		valid bool
		want  uint64
	}{
		{"18446744073709551615", true, ^uint64(0)},
		{"1.8446744073709551615e19", true, ^uint64(0)},
		{"81910e-1", true, 8191}, {"-0", true, 0}, {"0e999999999999999999999", true, 0},
		{"18446744073709551616", false, 0}, {"1e99999999999999999999", false, 0},
		{"1e-99999999999999999999", false, 0}, {"0.1", false, 0}, {"-1", false, 0}, {"null", false, 0},
	} {
		got, err := teamPermissionWord(json.RawMessage(scenario.raw))
		if (err == nil) != scenario.valid || scenario.valid && got != scenario.want {
			t.Fatalf("%s: got=%d error=%v", scenario.raw, got, err)
		}
	}
}

func TestDeletionGroupsAdmissionPagesAndFailure(t *testing.T) {
	for _, failure := range []string{"", "absent", "member", "page", "remove"} {
		t.Run(failure, func(t *testing.T) {
			user, team := gocql.TimeUUID(), gocql.TimeUUID()
			group := deletionGroupCursor{id: gocql.TimeUUID(), created: time.Now().UTC().Truncate(time.Millisecond)}
			var calls []string
			s := accountDeletionGroups{
				member: func(_ context.Context, u, tid gocql.UUID, rid string) (bool, error) {
					calls = append(calls, "member")
					if u != user || tid != team || rid != "synthetic-delete" {
						t.Error("incorrect member request")
					}
					if failure == "member" {
						return false, errors.New("synthetic")
					}
					return failure != "absent", nil
				},
				page: func(_ context.Context, tid gocql.UUID, cursor *deletionGroupCursor) ([]deletionGroupCursor, error) {
					calls = append(calls, "page")
					if tid != team {
						t.Error("incorrect team")
					}
					if failure == "page" {
						return nil, errors.New("synthetic")
					}
					if cursor == nil {
						return []deletionGroupCursor{group}, nil
					}
					if *cursor != group {
						t.Error("wrong cursor")
					}
					return nil, nil
				},
				remove: func(_ context.Context, g, u gocql.UUID) error {
					calls = append(calls, "remove")
					if g != group.id || u != user {
						t.Error("incorrect removal")
					}
					if failure == "remove" {
						return errors.New("synthetic")
					}
					return nil
				},
			}
			err := s.execute(context.Background(), user, team, "synthetic-delete")
			want := map[string][]string{"": {"member", "page", "remove", "page"}, "absent": {"member"}, "member": {"member"}, "page": {"member", "page"}, "remove": {"member", "page", "remove"}}[failure]
			if (err != nil) != (failure != "" && failure != "absent") || !reflect.DeepEqual(calls, want) {
				t.Fatalf("calls=%v error=%v", calls, err)
			}
		})
	}
}
