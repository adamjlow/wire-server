// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"os"
	"path/filepath"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestDualSourceConfigurationAndSecurityNegatives(t *testing.T) {
	workerPath := "../../../services/background-worker/background-worker.integration.yaml"
	galleyPath := "../../../services/galley/galley.integration.yaml"
	c, g, err := LoadConfig(workerPath, galleyPath)
	if err != nil || c.HTTP.Port != 8089 || g.Settings.Domain != "example.com" || c.Jobs.Threads != 1 || c.BackgroundJobs.Concurrency != 4 {
		t.Fatalf("source configuration failed: %v", err)
	}
	for _, tc := range []struct {
		name   string
		change func(*Config)
	}{
		{"missing-storage-location", func(c *Config) { c.Storage.PasswordReset = "" }},
		{"invalid-storage-location", func(c *Config) { c.Storage.Conversation = "migration" }},
		{"zero-migration-page", func(c *Config) { c.MigrationOptions.PageSize = 0 }},
		{"zero-migration-timeout", func(c *Config) { c.MigrationOptions.Timeout = 0 }},
		{"tls-bypass", func(c *Config) { c.Rabbit.Insecure = true }},
		{"negative-backoff", func(c *Config) { c.Pusher.Min = -1 }},
		{"invalid-port", func(c *Config) { c.HTTP.Port = 65536 }},
		{"unbounded-concurrency", func(c *Config) { c.BackgroundJobs.Concurrency = 1001 }},
		{"unknown-jitter", func(c *Config) { c.Jobs.Jitter = "unknown" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := c
			tc.change(&changed)
			b, err := yaml.Marshal(changed)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "worker.yaml")
			if err = os.WriteFile(path, b, 0600); err != nil {
				t.Fatal(err)
			}
			if _, _, err = LoadConfig(path, galleyPath); err == nil {
				t.Fatal("unsafe configuration accepted")
			}
		})
	}
	if _, _, err = LoadConfig(workerPath, filepath.Join(t.TempDir(), "absent.yaml")); err == nil {
		t.Fatal("galley config silently omitted")
	}
	if _, err = verifiedTLS("localhost", filepath.Join(t.TempDir(), "absent-ca.pem")); err == nil {
		t.Fatal("missing trusted CA accepted")
	}
}
