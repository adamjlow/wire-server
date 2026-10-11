// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type testProperties struct {
	rows      map[string][]byte
	calls     []string
	failWrite bool
	n         int64
}

func (s *testProperties) lookup(_ context.Context, _ gocql.UUID, k string) ([]byte, bool, error) {
	s.calls = append(s.calls, "lookup")
	v, ok := s.rows[k]
	return v, ok, nil
}
func (s *testProperties) count(context.Context, gocql.UUID) (int64, error) {
	s.calls = append(s.calls, "count")
	return s.n, nil
}
func (s *testProperties) write(_ context.Context, _ gocql.UUID, k string, v []byte) error {
	s.calls = append(s.calls, "write")
	if s.failWrite {
		return errors.New("synthetic failure")
	}
	s.rows[k] = append([]byte{}, v...)
	return nil
}
func (s *testProperties) remove(_ context.Context, _ gocql.UUID, k string) error {
	delete(s.rows, k)
	return nil
}
func (s *testProperties) clear(context.Context, gocql.UUID) error {
	s.rows = map[string][]byte{}
	return nil
}
func (s *testProperties) keys(context.Context, gocql.UUID) ([]string, error) {
	v := []string{}
	for k := range s.rows {
		v = append(v, k)
	}
	return v, nil
}
func (s *testProperties) all(context.Context, gocql.UUID) (map[string][]byte, error) {
	return s.rows, nil
}

func TestPropertyMutationPrecedenceAndNotification(t *testing.T) {
	user := gocql.TimeUUID()
	for _, tc := range []struct {
		name, key, raw string
		n              int64
		exists, fail   bool
		want           error
		calls          []string
	}{
		{"key first", "12345", "{", 16, false, false, errPropertyKeyLarge, nil},
		{"count before JSON", "k", "{", 16, false, false, errPropertyCount, []string{"lookup", "count"}},
		{"size before JSON", "k", "123456789012345678901", 0, false, false, errPropertyValueLarge, []string{"lookup", "count"}},
		{"parse before write", "k", "{", 0, false, false, errPropertyInvalid, []string{"lookup", "count"}},
		{"existing at limit", "k", " {\"x\":1,\"x\":2} ", 16, true, false, nil, []string{"lookup", "write", "notify"}},
		{"store failure", "k", "null", 0, false, true, nil, []string{"lookup", "count", "write"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &testProperties{rows: map[string][]byte{}, n: tc.n, failWrite: tc.fail}
			if tc.exists {
				store.rows[tc.key] = []byte("false")
			}
			var payload []byte
			s := propertyService{store: store, maxKey: 4, maxValue: 20, notify: func(_ context.Context, b []byte, id string) {
				store.calls = append(store.calls, "notify")
				payload = b
				if id != "synthetic-id" {
					t.Fatal(id)
				}
			}}
			err := s.set(context.Background(), user, tc.key, []byte(tc.raw), "synthetic-connection", "synthetic-id")
			if tc.fail {
				if err == nil {
					t.Fatal("write failure ignored")
				}
			} else if !errors.Is(err, tc.want) {
				t.Fatalf("error=%v want=%v", err, tc.want)
			}
			if !reflect.DeepEqual(store.calls, tc.calls) {
				t.Fatalf("calls=%v want=%v", store.calls, tc.calls)
			}
			if payload != nil {
				if string(store.rows[tc.key]) != tc.raw {
					t.Fatal("raw bytes changed")
				}
				var pushes []map[string]any
				if json.Unmarshal(payload, &pushes) != nil || len(pushes) != 1 {
					t.Fatal("invalid push")
				}
				p := pushes[0]
				if p["origin"] != user.String() || p["origin_connection"] != "synthetic-connection" || p["is_cells_event"] != false {
					t.Fatal(p)
				}
				event := p["payload"].([]any)[0].(map[string]any)
				if event["type"] != "user.properties-set" || event["value"].(map[string]any)["x"] != float64(1) {
					t.Fatal(event)
				}
				rcpt := p["recipients"].([]any)[0].(map[string]any)
				if rcpt["route"] != "direct" || rcpt["user_id"] != user.String() || len(rcpt["clients"].([]any)) != 0 {
					t.Fatal(rcpt)
				}
			}
		})
	}
}

func TestPropertyRawReadsAndInvalidStoredMap(t *testing.T) {
	user := gocql.TimeUUID().String()
	store := &testProperties{rows: map[string][]byte{"raw": []byte(" \n{\"x\":1,\"x\":2,\"large\":9007199254740993}\t"), "bad": []byte("{")}}
	s := newPropertyService(Config{}, store, nil)
	for _, tc := range []struct {
		path   string
		status int
		body   string
	}{
		{"/properties/raw", 200, string(store.rows["raw"])}, {"/properties/bad", 200, "{"}, {"/properties/missing", 404, ""}, {"/properties-values", 500, ""},
	} {
		r := httptest.NewRequest(http.MethodGet, tc.path, nil)
		r.Header.Set("Z-User", user)
		w := httptest.NewRecorder()
		if !s.handle(w, r, tc.path) {
			t.Fatal("missing route")
		}
		if w.Code != tc.status {
			t.Fatal(w.Code, w.Body.String())
		}
		if tc.status != 500 && w.Body.String() != tc.body {
			t.Fatal("raw read changed")
		}
	}
	delete(store.rows, "bad")
	r := httptest.NewRequest(http.MethodGet, "/properties-values", nil)
	r.Header.Set("Z-User", user)
	w := httptest.NewRecorder()
	s.handle(w, r, "/properties-values")
	if !strings.Contains(w.Body.String(), "9007199254740993") || strings.Count(w.Body.String(), `"x"`) != 1 {
		t.Fatal("decimal or duplicate semantics changed", w.Body.String())
	}
}

func TestPropertyHeadersEscapedKeysAndDeletionEvents(t *testing.T) {
	store := &testProperties{rows: map[string][]byte{}}
	var payloads [][]byte
	s := newPropertyService(Config{}, store, func(_ context.Context, b []byte, _ string) { payloads = append(payloads, b) })
	h := newHandler(Config{}, &brigReadHandlers{properties: s})
	user := gocql.TimeUUID().String()
	for _, tc := range []struct {
		method, path, body string
		conn               bool
		status             int
	}{
		{"PUT", "/properties/key", "true", false, 400}, {"PUT", "/properties/d%C3%B6ner", "true", true, 400},
		{"PUT", "/v1/properties/a%2Fb", "true", true, 200}, {"GET", "/properties/a%2Fb", "", false, 200},
		{"DELETE", "/properties/a%2Fb", "", true, 200}, {"DELETE", "/properties/absent", "", true, 200}, {"DELETE", "/properties", "", true, 200},
	} {
		r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		r.Header.Set("Z-User", user)
		r.Header.Set("Content-Type", "application/json")
		if tc.conn {
			r.Header.Set("Z-Connection", "synthetic")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("%s: %d %s", tc.path, w.Code, w.Body.String())
		}
	}
	if len(payloads) != 4 {
		t.Fatal("missing delete/clear event", len(payloads))
	}
	for _, raw := range []string{`"\ud800"`, `"\udc00"`, "true false", string([]byte{'"', 0xff, '"'})} {
		if _, err := parsePropertyValue([]byte(raw)); err == nil {
			t.Fatal("accepted malformed JSON")
		}
	}
}

func TestPropertyCapturedNativeParserAndMedia(t *testing.T) {
	for _, err := range []error{errPropertyKeyLarge, errPropertyValueLarge, errPropertyCount, errPropertyStoredInvalid} {
		w := httptest.NewRecorder()
		propertyHTTPError(w, err)
		if w.Header().Get("Content-Type") != "application/json" {
			t.Fatal("native HttpError media differs", w.Header())
		}
	}
	value, err := parsePropertyValue([]byte(`{"x":{"a":1,"a":2},"x":3,"list":[{"k":true,"k":false}],"empty":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	object := value.(map[string]any)
	if object["x"].(map[string]any)["a"] != json.Number("1") || object["list"].([]any)[0].(map[string]any)["k"] != true {
		t.Fatal(object)
	}
	_, err = parsePropertyValue([]byte(" \n{\t"))
	w := httptest.NewRecorder()
	propertyHTTPError(w, err)
	if w.Code != 400 || w.Header().Get("Content-Type") != "application/json" || !strings.Contains(w.Body.String(), "Unexpected end-of-input, expecting record key literal or }") {
		t.Fatal(w.Code, w.Header(), w.Body.String())
	}
	s := newPropertyService(Config{}, &testProperties{rows: map[string][]byte{}}, nil)
	for _, tc := range []struct {
		method, path, body, media string
		status                    int
	}{
		{"GET", "/properties/missing", "", "text/plain;charset=utf-8", 404},
		{"PUT", "/properties/key", "null", "", 200},
		{"DELETE", "/properties/key", "", "", 200},
	} {
		r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		r.Header.Set("Z-User", gocql.TimeUUID().String())
		r.Header.Set("Z-Connection", "synthetic")
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.handle(w, r, tc.path)
		if w.Code != tc.status || w.Header().Get("Content-Type") != tc.media || w.Body.Len() != 0 {
			t.Fatal(w.Code, w.Header(), w.Body.String())
		}
	}
}
