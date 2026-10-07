// SPDX-License-Identifier: AGPL-3.0-or-later
package proxy

import (
	"strings"
	"testing"
)

const yamlConfig = `proxy:
  host: 0.0.0.0
  port: 8080
secretsConfig: /fixture/secrets.conf
httpPoolSize: 10
maxConns: 100
logLevel: Warn
logNetStrings: false
disabledAPIVersions: [development, v0, v19]
giphyEndpoint: {host: provider, port: 8443}
`

func TestExistingConfigShapeAndUnknownOptions(t *testing.T) {
	cfg, err := decodeConfig(strings.NewReader(yamlConfig + "futureOption: ignored\n"))
	if err != nil || cfg.Proxy.Port != 8080 || cfg.GiphyEndpoint == nil || cfg.GiphyEndpoint.Host != "provider" ||
		cfg.HTTPPoolSize != 10 || cfg.LogNetStrings == nil || *cfg.LogNetStrings {
		t.Fatalf("existing config did not load: %v", err)
	}
}

func TestMalformedConfigNeverDisclosesInput(t *testing.T) {
	for _, value := range []string{
		"synthetic-secret: [unterminated",
		strings.Replace(yamlConfig, "logLevel: Warn", "logLevel: true", 1),
		strings.Replace(yamlConfig, "httpPoolSize: 10", "httpPoolSize: synthetic-secret", 1),
		strings.Replace(yamlConfig, "secretsConfig: /fixture/secrets.conf", "secretsConfig: null", 1),
		strings.Replace(yamlConfig, "[development, v0, v19]", "[v99]", 1),
		yamlConfig + "proxy: {host: synthetic-secret, port: 8080}\n",
	} {
		if _, err := decodeConfig(strings.NewReader(value)); err != errConfig || strings.Contains(err.Error(), "synthetic-secret") {
			t.Fatal("invalid config accepted or details disclosed")
		}
	}
}
