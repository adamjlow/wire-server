// SPDX-License-Identifier: AGPL-3.0-or-later
package proxy

import (
	"encoding/json"
	"errors"
	"io"
	"os"

	"go.yaml.in/yaml/v3"
)

// Endpoint preserves the Haskell option shape used by existing Helm configs.
type Endpoint struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

type Config struct {
	Proxy               Endpoint  `yaml:"proxy"`
	GiphyEndpoint       *Endpoint `yaml:"giphyEndpoint"`
	YoutubeEndpoint     *Endpoint `yaml:"youtubeEndpoint"`
	GoogleMapsEndpoint  *Endpoint `yaml:"googleMapsEndpoint"`
	SpotifyEndpoint     *Endpoint `yaml:"spotifyEndpoint"`
	SoundcloudEndpoint  *Endpoint `yaml:"soundcloudEndpoint"`
	SecretsConfig       string    `yaml:"secretsConfig"`
	HTTPPoolSize        int       `yaml:"httpPoolSize"`
	MaxConns            int       `yaml:"maxConns"`
	LogLevel            string    `yaml:"logLevel"`
	LogNetStrings       *bool     `yaml:"logNetStrings"`
	LogFormat           *string   `yaml:"logFormat"`
	DisabledAPIVersions []string  `yaml:"disabledAPIVersions"`
	DisableTLSForTest   *bool     `yaml:"disableTlsForTest"`
}

var errConfig = errors.New("invalid proxy configuration")

func decodeConfig(reader io.Reader) (Config, error) {
	var node yaml.Node
	decoder := yaml.NewDecoder(reader)
	if decoder.Decode(&node) != nil || len(node.Content) != 1 || node.Content[0].Kind != yaml.MappingNode {
		return Config{}, errConfig
	}
	var config Config
	// Decode through JSON-compatible values so YAML's typed-string coercions do
	// not turn booleans/numbers into Text options that Aeson's FromJSON rejects.
	var values any
	if node.Decode(&values) != nil {
		return Config{}, errConfig
	}
	encoded, err := json.Marshal(values)
	if err != nil || json.Unmarshal(encoded, &config) != nil {
		// YAML errors can quote secret-bearing input: return only a fixed error.
		return Config{}, errConfig
	}
	fields := map[string]*yaml.Node{}
	for i := 0; i < len(node.Content[0].Content); i += 2 {
		fields[node.Content[0].Content[i].Value] = node.Content[0].Content[i+1]
	}
	for _, name := range []string{"proxy", "secretsConfig", "httpPoolSize", "maxConns", "logLevel", "disabledAPIVersions"} {
		if value := fields[name]; value == nil || value.Tag == "!!null" {
			return Config{}, errConfig
		}
	}
	rootValues, ok := values.(map[string]any)
	if !ok {
		return Config{}, errConfig
	}
	for _, name := range []string{"proxy", "giphyEndpoint", "youtubeEndpoint", "googleMapsEndpoint", "spotifyEndpoint", "soundcloudEndpoint"} {
		value, present := rootValues[name]
		if name != "proxy" && (!present || value == nil) {
			continue
		}
		endpoint, ok := value.(map[string]any)
		if !ok || endpoint["host"] == nil || endpoint["port"] == nil {
			return Config{}, errConfig
		}
	}
	for _, version := range config.DisabledAPIVersions {
		if version != "development" && !knownVersion(version) {
			return Config{}, errConfig
		}
	}
	if !member(config.LogLevel, logLevels) || (config.LogFormat != nil && !member(*config.LogFormat, logFormats)) {
		return Config{}, errConfig
	}
	return config, nil
}

func LoadConfig(filename string) (Config, error) {
	file, err := os.Open(filename)
	if err != nil {
		return Config{}, errConfig
	}
	defer file.Close()
	return decodeConfig(file)
}

func knownVersion(version string) bool {
	for _, value := range []string{"v0", "v1", "v2", "v3", "v4", "v5", "v6", "v7", "v8", "v9",
		"v10", "v11", "v12", "v13", "v14", "v15", "v16", "v17", "v18", "v19"} {
		if version == value {
			return true
		}
	}
	return false
}
