package gundeck

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"go.yaml.in/yaml/v3"
	"net"
	"os"
	"strconv"
)

type Endpoint struct {
	Host string `yaml:"host"`
	Port uint16 `yaml:"port"`
}

func (e Endpoint) Address() string { return net.JoinHostPort(e.Host, strconv.Itoa(int(e.Port))) }

type RedisEndpoint struct {
	Endpoint  `yaml:",inline"`
	Mode      string `yaml:"connectionMode"`
	EnableTLS bool   `yaml:"enableTls"`
	CA        string `yaml:"tlsCa"`
	Insecure  bool   `yaml:"insecureSkipVerifyTls"`
}
type Config struct {
	Gundeck    Endpoint       `yaml:"gundeck"`
	Brig       Endpoint       `yaml:"brig"`
	Redis      RedisEndpoint  `yaml:"redis"`
	Additional *RedisEndpoint `yaml:"redisAdditionalWrite"`
	Cassandra  struct {
		Endpoint   Endpoint `yaml:"endpoint"`
		Keyspace   string   `yaml:"keyspace"`
		TLSCA      string   `yaml:"tlsCa"`
		Datacentre string   `yaml:"filterNodesByDatacentre"`
	} `yaml:"cassandra"`
	AWS struct {
		Account string `yaml:"account"`
		Region  string `yaml:"region"`
		ArnEnv  string `yaml:"arnEnv"`
		Queue   string `yaml:"queueName"`
		SNS     string `yaml:"snsEndpoint"`
		SQS     string `yaml:"sqsEndpoint"`
	} `yaml:"aws"`
	RabbitMQ struct {
		Endpoint  `yaml:",inline"`
		VHost     string `yaml:"vHost"`
		EnableTLS bool   `yaml:"enableTls"`
		CA        string `yaml:"caCert"`
		Insecure  bool   `yaml:"insecureSkipVerifyTls"`
	} `yaml:"rabbitmq"`
	Settings struct {
		HTTPPool          int    `yaml:"httpPoolSize"`
		TTL               uint32 `yaml:"notificationTTL"`
		NativeConcurrency *int   `yaml:"perNativePushConcurrency"`
		NativeBudget      *struct {
			Hard *int `yaml:"hard"`
			Soft *int `yaml:"soft"`
		} `yaml:"maxConcurrentNativePushes"`
		SQSThrottle  *int     `yaml:"sqsThrottleMillis"`
		Disabled     []string `yaml:"disabledAPIVersions"`
		PayloadLimit *int32   `yaml:"maxPayloadLoadSize"`
		PageSize     *int32   `yaml:"internalPageSize"`
		CellsQueue   *string  `yaml:"cellsEventQueue"`
		Consumable   bool     `yaml:"consumableNotifications"`
	} `yaml:"settings"`
	LogLevel      string `yaml:"logLevel"`
	LogNetStrings bool   `yaml:"logNetStrings"`
	LogFormat     string `yaml:"logFormat"`
}

func LoadConfig(path string) (Config, error) {
	var c Config
	data, err := os.ReadFile(path)
	if err != nil {
		return c, fmt.Errorf("configuration could not be read")
	}
	var fields map[string]any
	if err = yaml.Unmarshal(data, &fields); err != nil {
		return c, fmt.Errorf("invalid configuration")
	}
	for _, key := range []string{"gundeck", "brig", "redis", "cassandra", "aws", "rabbitmq", "settings", "logLevel"} {
		if _, ok := fields[key]; !ok {
			return c, fmt.Errorf("missing configuration field %s", key)
		}
	}
	settings, ok := fields["settings"].(map[string]any)
	if !ok {
		return c, fmt.Errorf("invalid settings")
	}
	if _, ok = settings["notificationTTL"]; !ok {
		return c, fmt.Errorf("missing settings.notificationTTL")
	}
	if err = yaml.Unmarshal(data, &c); err != nil {
		return c, fmt.Errorf("invalid typed configuration")
	}
	if c.Redis.Insecure || c.RabbitMQ.Insecure || (c.Additional != nil && c.Additional.Insecure) {
		return c, fmt.Errorf("TLS verification is required")
	}
	if c.Gundeck.Host == "" || c.Gundeck.Port == 0 {
		return c, fmt.Errorf("invalid bind endpoint")
	}
	return c, nil
}
func verifiedTLS(host, ca string) (*tls.Config, error) {
	var roots *x509.CertPool
	if ca != "" {
		data, err := os.ReadFile(ca)
		if err != nil {
			return nil, fmt.Errorf("trusted CA could not be read")
		}
		roots = x509.NewCertPool()
		if !roots.AppendCertsFromPEM(data) {
			return nil, fmt.Errorf("invalid trusted CA")
		}
	}
	return &tls.Config{ServerName: host, RootCAs: roots, MinVersion: tls.VersionTLS12}, nil
}
