// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func TestClientActivityNativeEpochWeek(t *testing.T) {
	for _, tc := range []struct{ second, want int64 }{{0, 604800}, {604799, 604800}, {604800, 1209600}, {-1, 0}, {-604800, 0}} {
		got := clientActivityTime(time.Unix(tc.second, 999999999))
		if got.Unix() != tc.want || got.Nanosecond() != 0 || got.Location() != time.UTC {
			t.Fatalf("second %d: %v", tc.second, got)
		}
	}
}

func TestClientActivityInternalCallback(t *testing.T) {
	user := gocql.TimeUUID()
	for _, failed := range []bool{false, true} {
		calls := 0
		s := clientActivityService{now: func() time.Time { return time.Unix(604800, 123) }, update: func(_ context.Context, u gocql.UUID, id string, at time.Time) error {
			calls++
			if u != user || id != "a" || at.Unix() != 1209600 || at.Nanosecond() != 0 {
				t.Fatal("activity arguments changed")
			}
			if failed {
				return errors.New("synthetic storage failure")
			}
			return nil
		}}
		h := newHandler(Config{}, &brigReadHandlers{clientActivity: &s})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", "/i/clients/"+user.String()+"/000A/activity", nil))
		if calls != 1 {
			t.Fatal("callback missing")
		}
		if !failed && (w.Code != 200 || w.Body.Len() != 0 || w.Header().Get("Content-Type") != "") {
			t.Fatal("native empty activity response changed")
		}
		if failed && w.Code != 500 {
			t.Fatal("storage failure hidden")
		}
		for _, path := range []string{"/i/clients/invalid/a/activity", "/i/clients/" + user.String() + "/invalid/activity"} {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("POST", path, nil))
			if w.Code != 400 || calls != 1 {
				t.Fatal("invalid capture reached storage")
			}
		}
	}
}
