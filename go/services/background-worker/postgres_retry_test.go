// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestPostgresAdministrativeRetriesAreFiniteAndErrorsStayGeneric(t *testing.T) {
	for _, code := range []string{"57P01", "57P02", "57P03", "57P04", "40001", "23505"} {
		calls, resets := 0, 0
		err := retryPostgres(t.Context(), func() { resets++ }, func() error {
			calls++
			return postgresFailure(&pgconn.PgError{Code: code, Message: "synthetic-secret", Detail: "synthetic-private-value"}, "database operation failed")
		})
		wantCalls, wantResets := 1, 0
		if strings.HasPrefix(code, "57P0") {
			wantCalls, wantResets = 6, 5
		}
		if calls != wantCalls || resets != wantResets || err == nil || strings.Contains(err.Error(), "synthetic") {
			t.Fatal("source reset budget changed or server details leaked")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := retryPostgres(ctx, func() { t.Fatal("cancelled session reset pool") }, func() error { t.Fatal("cancelled session executed"); return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled PostgreSQL session resumed")
	}
}

func TestDomainSourceLocalhostAndMalformedBoundaries(t *testing.T) {
	for _, domain := range []string{"localhost", "LOCALHOST", "Example.COM", "9.example.com"} {
		if !validDomain(domain) {
			t.Fatal("valid source Domain rejected")
		}
	}
	for _, domain := range []string{"127.0.0.1", "example.1com", "dotless", "evil/host", "example.com\r\nInjected: value"} {
		if validDomain(domain) {
			t.Fatal("invalid source Domain accepted")
		}
	}
}
