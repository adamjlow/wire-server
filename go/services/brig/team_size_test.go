// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func TestTeamSizePrimaryIndexAndNativeLegacyQuery(t *testing.T) {
	team, err := gocql.ParseUUID("00000000-0000-4000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/primary/user/_search" || r.Header.Get("Request-Id") != "synthetic-size" || r.Header.Get("Content-Type") != "application/json" {
			t.Error("incorrect native search request", r.Method, r.URL.Path)
		}
		var got, want any
		if json.NewDecoder(r.Body).Decode(&got) != nil {
			t.Fatal("invalid query JSON")
		}
		if json.Unmarshal([]byte(`{"size":0,"aggs":{"regulars":{"filter":{"bool":{"must":[{"term":{"team":"00000000-0000-4000-8000-000000000001"}},{"bool":{"should":[{"term":{"type":"regular"}},{"bool":{"must_not":[{"exists":{"field":"type"}}]}}]}}]}}},"apps":{"filter":{"bool":{"must":[{"term":{"team":"00000000-0000-4000-8000-000000000001"}},{"term":{"type":"app"}}]}}}}}`), &want) != nil {
			t.Fatal("invalid expected query")
		}
		if !reflect.DeepEqual(got, want) {
			t.Error("native regular/legacy/app aggregation mismatch", got)
		}
		_, _ = w.Write([]byte(`{"aggregations":{"regulars":{"doc_count":17},"apps":{"doc_count":3}}}`))
	}))
	defer server.Close()
	additional := "must-not-read"
	writer, err := newSearchIndex(searchIndexConfig{URL: server.URL, Index: "primary", AdditionalIndex: &additional})
	if err != nil {
		t.Fatal(err)
	}
	defer writer.close()
	size, err := writer.teamSize(context.Background(), team, "synthetic-size")
	if err != nil || size.Regulars.String() != "17" || size.Apps.String() != "3" || size.Total.String() != "20" {
		t.Fatal("wrong native counts", size, err)
	}
}

func TestTeamSizeNativeMissingMalformedAndNaturalCounts(t *testing.T) {
	for _, test := range []struct{ body, regulars, apps, total string }{
		{`{}`, "0", "0", "0"},
		{`{"aggregations":{"regulars":{"doc_count":-1},"apps":{"doc_count":"secret"}}}`, "0", "0", "0"},
		{`{"aggregations":{"regulars":{"doc_count":1.5},"apps":{"other":4}}}`, "0", "0", "0"},
		{`{"aggregations":{"regulars":{"doc_count":18446744073709551616},"apps":{"doc_count":1}}}`, "18446744073709551616", "1", "18446744073709551617"},
		{`{"aggregations":{"regulars":{"doc_count":3e1},"apps":{"doc_count":20.0e-1}}}`, "30", "2", "32"},
		{`{"aggregations":{"regulars":{"doc_count":1.8446744073709551616e19},"apps":{"doc_count":0e-99}}}`, "18446744073709551616", "0", "18446744073709551616"},
		{`{"aggregations":{"regulars":{"doc_count":1e-99},"apps":{"doc_count":-1e1}}}`, "0", "0", "0"},
	} {
		t.Run(test.body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(test.body)) }))
			defer server.Close()
			writer, err := newSearchIndex(searchIndexConfig{URL: server.URL, Index: "primary"})
			if err != nil {
				t.Fatal(err)
			}
			defer writer.close()
			size, err := writer.teamSize(context.Background(), gocql.UUID{}, "")
			if err != nil || size.Regulars.String() != test.regulars || size.Apps.String() != test.apps || size.Total.String() != test.total {
				t.Fatal("native zero fallback/natural counts changed", size, err)
			}
		})
	}
}

func TestTeamSizeHTTPReadAndErrorPropagation(t *testing.T) {
	for _, status := range []int{200, 503} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"aggregations":{"regulars":{"doc_count":2},"apps":{"doc_count":1}}}`))
		}))
		writer, err := newSearchIndex(searchIndexConfig{URL: server.URL, Index: "primary"})
		if err != nil {
			t.Fatal(err)
		}
		handler := newHandler(Config{}, &brigReadHandlers{index: writer})
		for _, method := range []string{"GET", "HEAD"} {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(method, "/i/teams/00000000-0000-4000-8000-000000000001/size", nil))
			if status == 200 {
				if recorder.Code != 200 || recorder.Header().Get("Content-Type") != "application/json;charset=utf-8" || method == "GET" && strings.TrimSpace(recorder.Body.String()) != `{"teamSizeRegulars":2,"teamSizeApps":1,"teamSize":3}` || method == "HEAD" && recorder.Body.Len() != 0 {
					t.Fatal("team-size response mismatch", recorder)
				}
			} else if recorder.Code != 500 || strings.Contains(recorder.Body.String(), "aggregations") {
				t.Fatal("upstream failure hidden or disclosed", recorder)
			}
		}
		writer.close()
		server.Close()
	}
}
