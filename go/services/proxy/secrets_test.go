// SPDX-License-Identifier: AGPL-3.0-or-later
package proxy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfiguratorGroupsImportsEscapesAndScopedInterpolation(t *testing.T) {
	dir := t.TempDir()
	write := func(name, text string) {
		t.Helper()
		if os.WriteFile(filepath.Join(dir, name), []byte(text), 0600) != nil {
			t.Fatal("fixture write failed")
		}
	}
	write("imported.conf", "giphy = \"synthetic-imported\"\n")
	write("main.conf", `base = "synthetic"
n = 12345678901234567890
secrets {
  import "imported.conf"
  spotify = "$(base)-$(n)-$$"
  soundcloud = "unicode-\uD83D\uDE00"
  youtube = "$(WIRE_SYNTHETIC_SECRET)"
  googlemaps = "escaped\nline"
}
`)
	t.Setenv("WIRE_SYNTHETIC_SECRET", "synthetic-env")
	s, err := LoadSecrets(filepath.Join(dir, "main.conf"))
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"giphy": "synthetic-imported", "spotify": "synthetic-12345678901234567890-$",
		"soundcloud": "unicode-😀", "youtube": "synthetic-env", "googlemaps": "escaped\nline"} {
		if got, ok := s.get(name); !ok || got != want {
			t.Fatalf("%s secret interpolation/escaping differs", name)
		}
	}
}

func TestSecretsRejectMalformedEscapesImportsAndNoValueLeaks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.conf")
	for _, text := range []string{
		`secrets { giphy = "synthetic\q" }`,
		`secrets { giphy = "synthetic\uD800" }`,
		`secrets { giphy = "$(WIRE_SYNTHETIC_MISSING_VARIABLE)" }`,
		`import "secrets.conf"`,
		"secrets {\ngiphy = [true,]\n}",
	} {
		if os.WriteFile(path, []byte(text), 0600) != nil {
			t.Fatal("fixture write failed")
		}
		if _, err := LoadSecrets(path); err != errSecrets {
			t.Fatal("invalid secret input accepted or detailed error returned")
		}
	}
}
