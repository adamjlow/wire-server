// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOriginalIntegrationConfigAndIngressForms(t *testing.T) {
	c, err := LoadConfig("../../../services/spar/spar.integration.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if c.SAML.Port != 8088 || c.Cassandra.Keyspace != "spar_test" || c.EnableEmailDiscovery == nil || *c.EnableEmailDiscovery {
		t.Fatal("original source config changed")
	}
	original := c.SAML.DomainConfig
	c.SAML.DomainConfig = DomainConfig{}
	c.SAML.Domains = map[string]DomainConfig{"example.com": original}
	if err = c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.SAML.DomainConfig = original
	if c.Validate() == nil {
		t.Fatal("mixed single/multi-ingress forms accepted")
	}
}
func TestConfigErrorsDoNotEchoSensitiveInput(t *testing.T) {
	file := filepath.Join(t.TempDir(), "spar.yaml")
	secret := "synthetic-secret-that-must-not-appear"
	if err := os.WriteFile(file, []byte("saml: [\ncredential: "+secret), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadConfig(file)
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatal("invalid input was accepted or leaked")
	}
	c, err := LoadConfig("../../../services/spar/spar.integration.yaml")
	if err != nil {
		t.Fatal(err)
	}
	c.Fingerprints = []string{strings.Repeat("aB : ", 20)}
	if c.Validate() != nil {
		t.Fatal("source whitespace/case fingerprint format rejected")
	}
	c.Fingerprints = []string{secret}
	if err = c.Validate(); err == nil || strings.Contains(err.Error(), secret) {
		t.Fatal("bad fingerprint accepted or leaked")
	}
}
