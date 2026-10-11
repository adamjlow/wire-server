// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/base64"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func TestConnectionHistoryAndNullableConversation(t *testing.T) {
	c := storedConnection{from: gocql.TimeUUID(), to: gocql.TimeUUID(), domain: "remote.example", relation: 9, updated: time.Unix(100, 123000000)}
	wire, err := c.wire()
	if err != nil || wire["status"] != "missing-legalhold-consent" || wire["last_update"] != "1970-01-01T00:01:40.123Z" {
		t.Fatal("native connection serialization changed")
	}
	if _, present := wire["conversation"]; present {
		t.Fatal("absent conversation emitted")
	}
	for _, relation := range []int32{-1, 12} {
		if _, err := connectionRelation(relation); err == nil {
			t.Fatal("invalid stored history accepted")
		}
	}
}

func TestConnectionPagingTableBoundariesAndAdmission(t *testing.T) {
	user := gocql.TimeUUID()
	calls := []bool{}
	localCount := 1
	localState := []byte(nil)
	s := connectionReads{page: func(_ context.Context, _ gocql.UUID, remote bool, _ *gocql.UUID, size int, state []byte) (connectionPage, error) {
		calls = append(calls, remote)
		if remote {
			if size != 1 {
				t.Fatal("remote remaining size differs")
			}
			return connectionPage{rows: []storedConnection{}, state: []byte{4}}, nil
		}
		rows := make([]storedConnection, localCount)
		for i := range rows {
			rows[i] = storedConnection{from: user, to: gocql.TimeUUID(), domain: "example.com", updated: time.Unix(0, 0)}
		}
		return connectionPage{rows: rows, state: localState}, nil
	}}
	result, err := s.paged(context.Background(), user, 2, 0, nil)
	if err != nil || len(calls) != 2 || calls[0] || !calls[1] || result["paging_state"] != base64.URLEncoding.EncodeToString([]byte{1, 4}) {
		t.Fatal("local then remote pagination differs")
	}
	calls = nil
	localCount = 2
	result, err = s.paged(context.Background(), user, 2, 0, nil)
	if err != nil || len(calls) != 1 || result["has_more"] != true || result["paging_state"] != "AA==" {
		t.Fatal("full local page must retain native has-more/table state")
	}
	calls = nil
	localState = []byte{2}
	localCount = 1
	result, err = s.paged(context.Background(), user, 2, 0, nil)
	if err != nil || len(calls) != 1 || result["paging_state"] != base64.URLEncoding.EncodeToString([]byte{0, 2}) {
		t.Fatal("local cursor switched tables early")
	}
	calls = nil
	for _, body := range []string{`{"size":0}`, `{"size":501}`, `{"paging_state":"Ag=="}`, `{"paging_state":""}`} {
		req := httptest.NewRequest("POST", "/list-connections", strings.NewReader(body))
		req.Header.Set("Z-User", user.String())
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		if !s.handle(w, req, "/list-connections") || w.Code != 400 {
			t.Fatal("invalid paging request accepted")
		}
	}
	if len(calls) != 0 {
		t.Fatal("invalid cursor/size reached storage")
	}
}
