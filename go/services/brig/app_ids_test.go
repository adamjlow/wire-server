// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func TestAppIDNativeFullRowValidation(t *testing.T) {
	id := "00000000-0000-4000-8000-000000000001"
	if err := validateAppIDRow(id, id, []byte(`{"unused":"retained native decoding"}`), "historical-category", strings.Repeat("é", 300), id); err != nil {
		t.Fatal(err)
	}
	for _, metadata := range []string{"null", "[]", "bad", `{"v":"\ud800"}`} {
		if validateAppIDRow(id, id, []byte(metadata), "other", "", id) == nil {
			t.Fatal("accepted invalid native app metadata", metadata)
		}
	}
	if validateAppIDRow(id, id, []byte(`{}`), "other", strings.Repeat("é", 301), id) == nil || validateAppIDRow(id, id, []byte(`{}`), "other", "", "invalid") == nil {
		t.Fatal("accepted invalid stored description/creator")
	}
}

func TestAppIDEndpointPreservesRowsAndPropagatesErrors(t *testing.T) {
	id := "00000000-0000-4000-8000-000000000001"
	for _, fail := range []bool{false, true} {
		calls := 0
		handler := newHandler(Config{}, &brigReadHandlers{appIDs: func(_ context.Context, team gocql.UUID) ([]string, error) {
			calls++
			if team.String() != id {
				t.Error("wrong team")
			}
			if fail {
				return nil, errors.New("synthetic private database detail")
			}
			return []string{id}, nil
		}})
		for _, method := range []string{"GET", "HEAD"} {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(method, "/i/teams/"+id+"/apps", nil))
			if fail {
				if recorder.Code != 500 || strings.Contains(recorder.Body.String(), "private") {
					t.Fatal("error hidden or disclosed", recorder)
				}
			} else if recorder.Code != 200 || recorder.Header().Get("Content-Type") != "application/json;charset=utf-8" || method == "GET" && strings.TrimSpace(recorder.Body.String()) != `["`+id+`"]` || method == "HEAD" && recorder.Body.Len() != 0 {
				t.Fatal("incorrect app IDs response", recorder)
			}
		}
		if calls != 2 {
			t.Fatal("HEAD skipped native action")
		}
	}
}
