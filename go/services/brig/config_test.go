// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSigningKeysUseNativeURLAlphabet(t *testing.T) {
	// RFC 8032's public test vector is synthetic and exercises both alphabets.
	seed, err := hex.DecodeString("9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60")
	if err != nil {
		t.Fatal(err)
	}
	private := ed25519.NewKeyFromSeed(seed)
	public := private.Public().(ed25519.PublicKey)
	for _, material := range [][]byte{private, public} {
		path := filepath.Join(t.TempDir(), "keys.txt")
		encoded := base64.URLEncoding.EncodeToString(material)
		if !strings.ContainsAny(encoded, "-_") {
			t.Fatal("synthetic key must exercise URL alphabet")
		}
		if err := os.WriteFile(path, []byte(encoded+"\n\n"+encoded+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		keys, err := readSigningKeys(path, len(material))
		if err != nil || len(keys) != 2 || !bytes.Equal(keys[0], material) || !bytes.Equal(keys[1], material) {
			t.Fatal("native URL-encoded signing keys did not round trip")
		}
	}
}

func TestOriginalConfigPreservesTemplatesAndStorageModes(t *testing.T) {
	c, err := LoadConfig("../../../services/brig/brig.integration.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if c.Brig.Port != 8082 || c.Settings.Domain != "example.com" || c.ZAuth.Settings.KeyIndex != 1 || c.PostgresPool.Size != 20 {
		t.Fatal("original options not decoded")
	}
	email := c.Other["emailSMS"].(map[string]any)
	if email["team"].(map[string]any)["tInvitationUrl"] != "https://example.com/join/?team-code=${code}" {
		t.Fatal("native template changed")
	}
	for _, mode := range []string{"cassandra", "postgresql", "migration-to-postgresql"} {
		c.Migration["user"] = mode
		if err = c.Validate(); err != nil {
			t.Fatalf("native mode %s: %v", mode, err)
		}
	}
	c.Migration["user"] = "wrong"
	if c.Validate() == nil {
		t.Fatal("accepted invalid storage mode")
	}
}

func TestPostgresOptionValuesCannotOverrideTLSOrLeakSecrets(t *testing.T) {
	c := Config{Postgres: map[string]string{"host": "localhost", "port": "5432", "user": "synthetic", "dbname": "synthetic", "sslmode": "verify-full", "password": `synthetic' sslmode='disable\value`}, PostgresPool: PostgresPoolConfig{Size: 3, Acquisition: Duration(time.Second), Idleness: Duration(time.Minute)}}
	config, err := postgresConfig(c)
	if err != nil {
		t.Fatal(err)
	}
	if config.ConnConfig.Password != c.Postgres["password"] || config.ConnConfig.TLSConfig == nil || config.ConnConfig.TLSConfig.InsecureSkipVerify {
		t.Fatal("libpq value escaped its field or disabled TLS")
	}
	path := filepath.Join(t.TempDir(), "password.yaml")
	if err = os.WriteFile(path, []byte("'synthetic-from-file'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c.PostgresPassword = &path
	config, err = postgresConfig(c)
	if err != nil || config.ConnConfig.Password != "synthetic-from-file" {
		t.Fatal("password override failed")
	}
	if c.Postgres["password"] != `synthetic' sslmode='disable\value` {
		t.Fatal("caller config mutated")
	}
	c.PostgresPassword = nil
	c.Postgres["port"] = "synthetic-secret"
	_, err = postgresConfig(c)
	if err == nil || strings.Contains(err.Error(), "synthetic-secret") {
		t.Fatal("invalid config leaked supplied value")
	}
}

func TestSigningKeysRejectInvalidMaterialWithoutDisclosure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.txt")
	for _, bad := range []string{"", "synthetic-secret\n", "AQ==\n"} {
		if err := os.WriteFile(path, []byte(bad), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := readSigningKeys(path, 32)
		if err == nil || strings.Contains(err.Error(), "synthetic-secret") {
			t.Fatal("invalid key accepted/disclosed")
		}
	}
}
