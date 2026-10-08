package cargohold

import (
	"fmt"
	"net/url"
	"os"
	"strings"

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
	LogLevel      string  `yaml:"logLevel"`
	LogNetStrings bool    `yaml:"logNetStrings"`
	LogFormat     *string `yaml:"logFormat"`
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
	if err = validateConfig(b, &c); err != nil {
		return c, err
	}
	return c, nil
}

func validateConfig(raw []byte, c *Config) error {
	var values map[string]any
	if err := yaml.Unmarshal(raw, &values); err != nil {
		return fmt.Errorf("invalid cargohold configuration")
	}
	required := map[string][]string{"": {"cargohold", "brig", "aws", "settings", "logLevel"}, "cargohold": {"host", "port"}, "brig": {"host", "port"}, "aws": {"s3Bucket", "s3Endpoint"}, "settings": {"maxTotalBytes", "maxTotalBytesStrict", "downloadLinkTTL", "assetAuditLogEnabled", "federationDomain", "disabledAPIVersions"}}
	for group, fields := range required {
		data := values
		if group != "" {
			var ok bool
			data, ok = values[group].(map[string]any)
			if !ok {
				return fmt.Errorf("invalid configuration section %s", group)
			}
		}
		for _, field := range fields {
			if value, ok := data[field]; !ok || value == nil {
				return fmt.Errorf("missing configuration field %s.%s", group, field)
			}
		}
	}
	if !validDomain(c.Settings.FederationDomain) {
		return fmt.Errorf("invalid federation domain")
	}
	c.Settings.FederationDomain = strings.ToLower(c.Settings.FederationDomain)
	if c.AWS.AddressingStyle != "" && c.AWS.AddressingStyle != "path" && c.AWS.AddressingStyle != "auto" && c.AWS.AddressingStyle != "virtual" {
		return fmt.Errorf("invalid S3 addressing style")
	}
	if c.AWS.Compatibility != "" && c.AWS.Compatibility != "scality-ring" {
		return fmt.Errorf("invalid S3 compatibility")
	}
	endpoints := []string{c.AWS.Endpoint}
	if c.AWS.DownloadEndpoint != "" {
		endpoints = append(endpoints, c.AWS.DownloadEndpoint)
	}
	ingress := map[string]string{}
	for host, ep := range c.AWS.MultiIngress {
		if !validDomain(host) {
			return fmt.Errorf("invalid multi-ingress domain")
		}
		ingress[strings.ToLower(host)] = ep
		endpoints = append(endpoints, ep)
	}
	c.AWS.MultiIngress = ingress
	for _, ep := range endpoints {
		u, err := url.Parse(ep)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return fmt.Errorf("invalid S3 endpoint")
		}
	}
	if len(c.AWS.MultiIngress) > 0 && (c.AWS.CloudFront != nil || c.AWS.DownloadEndpoint != "") {
		return fmt.Errorf("multi-ingress conflicts with CloudFront or a single download endpoint")
	}
	if c.AWS.CloudFront != nil && (!validDomain(c.AWS.CloudFront.Domain) || c.AWS.CloudFront.KeyPairID == "" || c.AWS.CloudFront.PrivateKey == "") {
		return fmt.Errorf("invalid CloudFront configuration")
	}
	for _, expr := range c.Settings.DisabledAPIVersions {
		text, ok := expr.(string)
		if !ok {
			return fmt.Errorf("invalid disabled API version")
		}
		found := text == "development"
		for v := 0; v <= 19; v++ {
			if text == fmt.Sprintf("v%d", v) {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("invalid disabled API version")
		}
	}
	if !member(c.LogLevel, logLevels) {
		return fmt.Errorf("invalid log level")
	}
	if c.LogFormat != nil && !member(*c.LogFormat, logFormats) {
		return fmt.Errorf("invalid log format")
	}
	return nil
}
