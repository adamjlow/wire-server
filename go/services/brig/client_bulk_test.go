// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func TestInternalClientSetsUseNativeNumericIDsAndAllRecordFields(t *testing.T) {
	ids, err := nativeClientIDSet([]string{"10", "02", "2", "000a", "a"})
	if err != nil || !reflect.DeepEqual(ids, []string{"2", "a", "10"}) {
		t.Fatal("native numeric ID set", ids, err)
	}
	base := storedClient{ID: "a", Type: 0, Created: time.Unix(1700000000, 0), Capabilities: []int32{2, 1}}
	alias := base
	alias.ID = "000a"
	alias.Capabilities = []int32{1, 2, 1}
	different := base
	different.Type = 1
	larger := base
	larger.ID = "10"
	set, err := nativeClientSet([]storedClient{larger, different, alias, base})
	if err != nil || len(set) != 3 || set[0].ID != "a" || set[0].Type != 0 || set[1].Type != 1 || set[2].ID != "10" {
		t.Fatal("native full-client record set lost variants/order", set, err)
	}
}

func TestInternalFullClientsExposeLatestSchemaAndEveryRequestedUser(t *testing.T) {
	var first, second gocql.UUID
	first[15] = 1
	second[15] = 2
	client := storedClient{ID: "ab", Type: 1, Created: time.Unix(1700000000, 0), Capabilities: []int32{2}, PublicKeys: map[string][]byte{"ed25519": {1, 2}}}
	var counted atomic.Int32
	bulk := clientBulkReads{clients: func(_ context.Context, id gocql.UUID) ([]storedClient, error) {
		counted.Add(1)
		if id == first {
			return []storedClient{client}, nil
		}
		return nil, nil
	}}
	handler := newHandler(Config{}, &brigReadHandlers{bulkClients: &bulk})
	body := `{"users":["` + second.String() + `","` + first.String() + `","` + first.String() + `"]}`
	request := httptest.NewRequest("POST", "/i/clients/full", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	var result map[string][]map[string]any
	if recorder.Code != 200 || recorder.Header().Get("Content-Type") != "application/json;charset=utf-8" || json.Unmarshal(recorder.Body.Bytes(), &result) != nil || len(result) != 2 || len(result[second.String()]) != 0 || len(result[first.String()]) != 1 || counted.Load() != 2 {
		t.Fatal("native user map", recorder)
	}
	object := result[first.String()][0]
	if !reflect.DeepEqual(object["capabilities"], []any{"consumable-notifications"}) || object["mls_public_keys"].(map[string]any)["ed25519"] != "AQI=" {
		t.Fatal("internal schema was public-version filtered", object)
	}
}

func TestInternalClientValidationBeforeStoreAndErrorsNotPartialSuccess(t *testing.T) {
	var calls atomic.Int32
	bulk := clientBulkReads{clients: func(context.Context, gocql.UUID) ([]storedClient, error) {
		calls.Add(1)
		return nil, errors.New("synthetic private store error")
	}, ids: func(context.Context, gocql.UUID) ([]string, error) {
		calls.Add(1)
		return nil, errors.New("synthetic private store error")
	}}
	handler := newHandler(Config{}, &brigReadHandlers{bulkClients: &bulk})
	for _, body := range []string{`null`, `{}`, `{"users":null}`, `{"users":[null]}`, `{"users":["invalid"]}`, `{"users":[]} {}`, `{"users":[],"unknown":"\ud800"}`} {
		request := httptest.NewRequest("POST", "/i/clients/full", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != 400 || calls.Load() != 0 {
			t.Fatal("malformed user set reached store", body)
		}
	}
	for _, path := range []string{"/i/clients", "/i/clients/full"} {
		request := httptest.NewRequest("POST", path, strings.NewReader(`{"users":["00000000-0000-0000-0000-000000000001"]}`))
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != 500 || strings.Contains(recorder.Body.String(), "private") {
			t.Fatal("store error masked or leaked", recorder)
		}
		request = httptest.NewRequest("POST", path, strings.NewReader(`{"users":[]}`))
		request.Header.Set("Content-Type", "application/json")
		recorder = httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != 200 || strings.TrimSpace(recorder.Body.String()) != "{}" {
			t.Fatal("empty UserSet rendering", recorder)
		}
	}
}

func TestInternalClientBulkNativeConcurrencyAndCancellation(t *testing.T) {
	for _, full := range []bool{false, true} {
		limit := 16
		if full {
			limit = 50
		}
		users := make([]gocql.UUID, limit+1)
		for i := range users {
			users[i][15] = byte(i + 1)
		}
		entered := make(chan struct{}, len(users))
		release := make(chan struct{})
		block := func(ctx context.Context) error {
			entered <- struct{}{}
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		bulk := clientBulkReads{clients: func(ctx context.Context, _ gocql.UUID) ([]storedClient, error) { return nil, block(ctx) }, ids: func(ctx context.Context, _ gocql.UUID) ([]string, error) { return nil, block(ctx) }}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { _, err := bulk.lookup(ctx, users, full); done <- err }()
		for i := 0; i < limit; i++ {
			select {
			case <-entered:
			case <-time.After(2 * time.Second):
				cancel()
				t.Fatal("native worker limit not reached")
			}
		}
		select {
		case <-entered:
			cancel()
			t.Fatal("native worker bound exceeded")
		default:
		}
		cancel()
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("cancellation hidden")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("bulk cancellation did not drain")
		}
	}
}
