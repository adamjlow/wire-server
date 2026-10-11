// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func TestDomainRegistrationGuard(t *testing.T) {
	ctx := context.Background()
	for tag := int32(1); tag <= 6; tag++ {
		var wanted error
		switch tag {
		case 3:
			wanted = errDomainRegistrationSSO
		case 4:
			wanted = errDomainRegistrationBackend
		case 5:
			wanted = errDomainRegistrationDisabled
		}
		got := guardRegistrationDomain(ctx, "synthetic@EXAMPLE.COM", func(_ context.Context, key string) (*domainRegistration, error) {
			if key != "example.com" {
				t.Fatal("domain not canonicalized")
			}
			return &domainRegistration{redirect: &tag}, nil
		})
		if !errors.Is(got, wanted) {
			t.Fatalf("tag %d: %v", tag, got)
		}
	}
	failed := errors.New("synthetic database failure")
	if got := guardRegistrationDomain(ctx, "synthetic@example.com", func(context.Context, string) (*domainRegistration, error) { return nil, failed }); !errors.Is(got, failed) {
		t.Fatal("storage failure ignored")
	}
	called := false
	if got := guardRegistrationDomain(ctx, "synthetic@123.456", func(context.Context, string) (*domainRegistration, error) { called = true; return nil, nil }); !errors.Is(got, errInvalidEmailDomain) || called {
		t.Fatal("invalid domain reached store")
	}
	if got := guardRegistrationDomain(ctx, "synthetic@example.com", func(context.Context, string) (*domainRegistration, error) { return nil, nil }); got != nil {
		t.Fatal(got)
	}
}
func TestNativeEmailDomainGrammar(t *testing.T) {
	for _, value := range []string{"EXAMPLE.COM", "localhost", "LocalHost", "1.example", "a.b", "a-b.example"} {
		got, err := parseEmailDomain(value)
		if err != nil || got != strings.ToLower(value) {
			t.Fatalf("valid domain %q", value)
		}
	}
	for _, value := range []string{"", "example", "a.1example", "127.0.0.1", "a..com", "a.com.", "-a.com", "a-.com", "a_b.com", "é.com", strings.Repeat("a", 64) + ".com", strings.Repeat("a.", 127) + "com"} {
		if _, err := parseEmailDomain(value); err == nil {
			t.Fatalf("invalid domain %q accepted", value)
		}
	}
}
func TestDomainRegistrationTypedVersusSemanticFailures(t *testing.T) {
	domain := "example.com"
	id := gocql.UUID{}
	tag := func(value int32) *int32 { return &value }
	for _, r := range []domainRegistration{
		{domain: &domain},
		{domain: &domain, redirect: tag(2), invite: tag(2)},
		{domain: &domain, redirect: tag(3), idp: &id, invite: tag(3), team: &id},
		{domain: &domain, redirect: tag(4), backend: []byte("https://example.com"), webapp: []byte("https://web.example.com")},
	} {
		if err := r.decode(); err != nil || !r.consistent() {
			t.Fatal("valid registration rejected", err)
		}
	}
	for _, r := range []domainRegistration{
		{domain: &domain, redirect: tag(3)},
		{domain: &domain, redirect: tag(1), idp: &id},
		{domain: &domain, redirect: tag(4)},
		{domain: &domain, invite: tag(3)},
		{domain: &domain, team: &id},
		{domain: &domain, webapp: []byte("https://example.com")},
	} {
		if err := r.decode(); err != nil || r.consistent() {
			t.Fatal("semantic mismatch not distinguished", err)
		}
	}
	token := "nonascii-\u0080"
	for _, r := range []domainRegistration{
		{}, {domain: &domain, redirect: tag(0)}, {domain: &domain, invite: tag(4)},
		{domain: &domain, backend: []byte("http://example.com")},
		{domain: &domain, backend: []byte("HTTPS://example.com")},
		{domain: &domain, backend: []byte("https://example.com/secret\n")},
		{domain: &domain, dnsToken: &token},
	} {
		if r.decode() == nil {
			t.Fatal("invalid stored field accepted")
		}
		if strings.Contains(r.String(), "secret") {
			t.Fatal("registration leaks credential")
		}
	}
}
