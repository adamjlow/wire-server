package cargohold

import (
	"fmt"
	"os"

	"go.yaml.in/yaml/v3"
)

type Endpoint struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

type Config struct {
	Cargohold Endpoint  `yaml:"cargohold"`
	Brig      Endpoint  `yaml:"brig"`
	Federator *Endpoint `yaml:"federator"`
	AWS       struct {
		Bucket           string            `yaml:"s3Bucket"`
		Endpoint         string            `yaml:"s3Endpoint"`
		DownloadEndpoint string            `yaml:"s3DownloadEndpoint"`
		AddressingStyle  string            `yaml:"s3AddressingStyle"`
		Compatibility    string            `yaml:"s3Compatibility"`
		MultiIngress     map[string]string `yaml:"multiIngress"`
		CloudFront       *struct {
			Domain     string `yaml:"domain"`
			KeyPairID  string `yaml:"keyPairId"`
			PrivateKey string `yaml:"privateKey"`
		} `yaml:"cloudFront"`
	} `yaml:"aws"`
	Settings struct {
		MaxTotalBytes        int64  `yaml:"maxTotalBytes"`
		MaxTotalBytesStrict  int64  `yaml:"maxTotalBytesStrict"`
		DownloadLinkTTL      uint64 `yaml:"downloadLinkTTL"`
		AssetAuditLogEnabled bool   `yaml:"assetAuditLogEnabled"`
		FederationDomain     string `yaml:"federationDomain"`
		DisabledAPIVersions  []any  `yaml:"disabledAPIVersions"`
	} `yaml:"settings"`
	LogLevel      string `yaml:"logLevel"`
	LogNetStrings bool   `yaml:"logNetStrings"`
}

func LoadConfig(path string) (Config, error) {
	var c Config
	b, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	if err = yaml.Unmarshal(b, &c); err != nil {
		return c, fmt.Errorf("invalid cargohold configuration: %w", err)
	}
	return c, nil
}
