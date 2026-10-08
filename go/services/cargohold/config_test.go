package cargohold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExistingConfigurationAndRequiredFields(t *testing.T) {
	file := "../../../services/cargohold/cargohold.integration.yaml"
	c, err := LoadConfig(file)
	if err != nil || c.Settings.MaxTotalBytes != 27262976 || c.AWS.Bucket != "dummy-bucket" {
		t.Fatalf("existing config: %+v %v", c, err)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		strings.Replace(string(data), "  assetAuditLogEnabled: false\n", "", 1),
		strings.Replace(string(data), "  federationDomain: example.com", "  federationDomain: 127.0.0.1", 1),
		strings.Replace(string(data), "  s3Bucket: dummy-bucket", "  s3AddressingStyle: unknown\n  s3Bucket: dummy-bucket", 1),
	} {
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err = os.WriteFile(path, []byte(bad), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err = LoadConfig(path); err == nil {
			t.Fatal("invalid configuration accepted")
		}
	}
}
