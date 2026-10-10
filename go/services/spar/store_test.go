// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestDiscoveryIncludesSparAndSeedContactsOnly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"roles":{"cassandra_spar":[{"privateIpAddress":"127.0.0.1"}],"cassandra_spar_seed":[{"privateIpAddress":"127.0.0.2"}],"cassandra_other":[{"privateIpAddress":"127.0.0.3"}]}}`))
	}))
	defer server.Close()
	hosts, err := contacts(context.Background(), Config{DiscoveryURL: server.URL})
	if err != nil || !reflect.DeepEqual(hosts, []string{"127.0.0.1", "127.0.0.2"}) {
		t.Fatalf("contacts %v, err %v", hosts, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = contacts(ctx, Config{DiscoveryURL: server.URL}); err == nil {
		t.Fatal("canceled discovery succeeded")
	}
}
func TestEmptyDiscoveryCannotBypassStartup(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"roles":{}}`)) }))
	defer server.Close()
	if _, err := contacts(context.Background(), Config{DiscoveryURL: server.URL}); err == nil {
		t.Fatal("discovery without contacts accepted")
	}
}
