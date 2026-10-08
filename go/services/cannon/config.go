package cannon

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

type Endpoint struct {
	Host string `yaml:"host"`
	Port uint16 `yaml:"port"`
}

func (e Endpoint) Address() string { return net.JoinHostPort(e.Host, strconv.Itoa(int(e.Port))) }

type Config struct {
	Cannon struct {
		Endpoint         `yaml:",inline"`
		ExternalHost     *string `yaml:"externalHost"`
		ExternalHostFile *string `yaml:"externalHostFile"`
	} `yaml:"cannon"`
	Gundeck  Endpoint `yaml:"gundeck"`
	RabbitMQ struct {
		Endpoint           `yaml:",inline"`
		VHost              string `yaml:"vHost"`
		EnableTLS          bool   `yaml:"enableTls"`
		CACert             string `yaml:"caCert"`
		InsecureSkipVerify bool   `yaml:"insecureSkipVerifyTls"`
	} `yaml:"rabbitmq"`
	Cassandra struct {
		Endpoint   Endpoint `yaml:"endpoint"`
		Keyspace   string   `yaml:"keyspace"`
		TLSCA      string   `yaml:"tlsCa"`
		Datacentre string   `yaml:"filterNodesByDatacentre"`
	} `yaml:"cassandra"`
	Drain struct {
		Grace    uint64 `yaml:"gracePeriodSeconds"`
		Interval uint64 `yaml:"millisecondsBetweenBatches"`
		MinBatch uint64 `yaml:"minBatchSize"`
	} `yaml:"drainOpts"`
	WS struct {
		Activity int64 `yaml:"activityTimeout"`
		Pong     int64 `yaml:"pongTimeout"`
	} `yaml:"wsOpts"`
	MaxConnections  int      `yaml:"rabbitMqMaxConnections"`
	MaxChannels     int      `yaml:"rabbitMqMaxChannels"`
	NotificationTTL int64    `yaml:"notificationTTL"`
	Disabled        []string `yaml:"disabledAPIVersions"`
	LogLevel        string   `yaml:"logLevel"`
	LogFormat       string   `yaml:"logFormat"`
	LogNetStrings   bool     `yaml:"logNetStrings"`
}

func LoadConfig(path string) (Config, error) {
	c := Config{MaxConnections: 1000, MaxChannels: 300}
	c.WS.Activity = 20000000
	c.WS.Pong = 20000000
	raw, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	if err = yaml.Unmarshal(raw, &c); err != nil {
		return c, fmt.Errorf("invalid cannon configuration: %w", err)
	}
	var values map[string]any
	if err = yaml.Unmarshal(raw, &values); err != nil {
		return c, fmt.Errorf("invalid cannon configuration")
	}
	required := map[string][]string{"": {"cannon", "gundeck", "rabbitmq", "cassandra", "drainOpts", "disabledAPIVersions", "notificationTTL", "logLevel"}, "cannon": {"host", "port"}, "gundeck": {"host", "port"}, "rabbitmq": {"host", "port", "vHost"}, "cassandra": {"endpoint", "keyspace"}, "drainOpts": {"gracePeriodSeconds", "millisecondsBetweenBatches", "minBatchSize"}}
	for group, fields := range required {
		data := values
		if group != "" {
			var ok bool
			data, ok = values[group].(map[string]any)
			if !ok {
				return c, fmt.Errorf("invalid configuration section %s", group)
			}
		}
		for _, field := range fields {
			if v, ok := data[field]; !ok || v == nil {
				return c, fmt.Errorf("missing configuration field %s.%s", group, field)
			}
		}
	}
	cs, ok := values["cassandra"].(map[string]any)
	if !ok {
		return c, fmt.Errorf("invalid cassandra configuration")
	}
	ep, ok := cs["endpoint"].(map[string]any)
	if !ok || ep["host"] == nil || ep["port"] == nil {
		return c, fmt.Errorf("missing Cassandra endpoint")
	}
	if c.MaxConnections <= 0 {
		return c, fmt.Errorf("rabbitMqMaxConnections must be strictly positive")
	}
	if c.MaxChannels <= 0 {
		return c, fmt.Errorf("rabbitMqMaxChannels must be strictly positive")
	}
	if c.Drain.Grace == 0 {
		return c, fmt.Errorf("drainOpts.gracePeriodSeconds must not be set to 0.")
	}
	if c.Drain.Interval == 0 {
		return c, fmt.Errorf("drainOpts.millisecondsBetweenBatches must not be set to 0.")
	}
	if c.Cannon.ExternalHost == nil {
		if c.Cannon.ExternalHostFile == nil {
			return c, fmt.Errorf("One of externalHost or externalHostFile must be defined")
		}
		b, err := os.ReadFile(*c.Cannon.ExternalHostFile)
		if err != nil {
			return c, fmt.Errorf("external host file unavailable")
		}
		h := strings.TrimSpace(string(b))
		c.Cannon.ExternalHost = &h
	}
	if c.RabbitMQ.InsecureSkipVerify {
		return c, fmt.Errorf("unverified RabbitMQ TLS configuration is unsupported; use a trusted CA")
	}
	for _, v := range c.Disabled {
		if v == "development" {
			continue
		}
		n, err := strconv.Atoi(strings.TrimPrefix(v, "v"))
		if err != nil || v != "v"+strconv.Itoa(n) || n < 0 || n > 19 {
			return c, fmt.Errorf("invalid disabled API version")
		}
	}
	switch c.LogLevel {
	case "Trace", "Debug", "Info", "Warn", "Error", "Fatal":
	default:
		return c, fmt.Errorf("invalid log level")
	}
	switch c.LogFormat {
	case "", "Plain", "Netstring", "JSON", "StructuredJSON":
	default:
		return c, fmt.Errorf("invalid log format")
	}
	return c, nil
}
func verifiedTLS(host, caFile string) (*tls.Config, error) {
	config := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: host}
	if caFile != "" {
		roots, err := x509.SystemCertPool()
		if err != nil {
			roots = x509.NewCertPool()
		}
		raw, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("TLS CA unavailable")
		}
		if !roots.AppendCertsFromPEM(raw) {
			return nil, fmt.Errorf("TLS CA invalid")
		}
		config.RootCAs = roots
	}
	return config, nil
}
