// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPostgresSourceOptionsPasswordFileAndInjectionProtection(t *testing.T) {
	c := PostgresPoolConfig{Size: 5, Acquisition: Duration(1100 * time.Millisecond), Idleness: Duration(10 * time.Minute)}
	g := GalleyConfig{Postgres: map[string]string{"host": "localhost", "port": "5432", "dbname": "synthetic", "user": "synthetic", "password": `synthetic' sslmode='disable\value`, "sslmode": "verify-full"}}
	config, err := postgresConfig(c, g)
	if err != nil {
		t.Fatal(err)
	}
	if config.ConnConfig.Password != g.Postgres["password"] || config.ConnConfig.TLSConfig == nil || config.ConnConfig.TLSConfig.InsecureSkipVerify || config.ConnConfig.TLSConfig.ServerName != "localhost" || config.MaxConns != 5 || config.MaxConnIdleTime != 10*time.Minute || config.MaxConnLifetime != 0 {
		t.Fatal("source connection options were changed or password became options")
	}
	file := filepath.Join(t.TempDir(), "password.yaml")
	if err := os.WriteFile(file, []byte("'synthetic-override'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	g.PostgresPassword = &file
	config, err = postgresConfig(c, g)
	if err != nil || config.ConnConfig.Password != "synthetic-override" {
		t.Fatal("source password-file override failed")
	}
	if g.Postgres["password"] != `synthetic' sslmode='disable\value` {
		t.Fatal("password override mutated input configuration")
	}
	store, err := openPostgres(t.Context(), c, g)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Pool.Close()
	// Pool creation is lazy; this test does not contact any host dependency.
	if store.Acquisition != 2*time.Second {
		t.Fatal("source acquisition timeout was not rounded up")
	}
}

func TestPostgresInvalidConfigAndIdentifiersDoNotExposeSecrets(t *testing.T) {
	c := PostgresPoolConfig{Size: 1, Acquisition: Duration(time.Second), Idleness: Duration(time.Minute)}
	for _, params := range []map[string]string{{"host": "localhost", "port": "synthetic-secret"}, {"host": "localhost", "sslmode": "synthetic-secret"}, {"host": "localhost", "bad key": "synthetic-secret"}, {"host": "localhost", "password": "synthetic-secret\x00"}} {
		_, err := postgresConfig(c, GalleyConfig{Postgres: params})
		if err == nil || strings.Contains(err.Error(), "synthetic-secret") {
			t.Fatal("invalid PostgreSQL option accepted or secret exposed")
		}
	}
	for _, id := range []string{"';DELETE FROM user_group;--", "not-a-uuid", ""} {
		if _, err := postgresUUID(id); err == nil {
			t.Fatal("untrusted identifier accepted")
		}
	}
	u, err := postgresUUID(jobUUID)
	if err != nil || postgresUUIDString(u) != jobUUID {
		t.Fatal("bound UUID changed")
	}
}
