// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"math"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

type Endpoint struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

func (e Endpoint) Address() string { return net.JoinHostPort(e.Host, strconv.Itoa(e.Port)) }
func (e Endpoint) valid() bool     { return e.Host != "" && e.Port > 0 && e.Port <= 65535 }

type Duration time.Duration

func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	v, err := time.ParseDuration(n.Value)
	if err != nil || v <= 0 {
		return errors.New("invalid positive duration")
	}
	*d = Duration(v)
	return nil
}

type RabbitConfig struct {
	Endpoint  `yaml:",inline"`
	VHost     string `yaml:"vHost"`
	AdminHost string `yaml:"adminHost"`
	AdminPort int    `yaml:"adminPort"`
	TLS       bool   `yaml:"enableTls"`
	CA        string `yaml:"caCert"`
	Insecure  bool   `yaml:"insecureSkipVerifyTls"`
}

type CassandraConfig struct {
	Endpoint   Endpoint `yaml:"endpoint"`
	Keyspace   string   `yaml:"keyspace"`
	TLSCA      string   `yaml:"tlsCa"`
	Datacentre string   `yaml:"filterNodesByDatacentre"`
}

type PostgresPoolConfig struct {
	Size        int      `yaml:"size"`
	Acquisition Duration `yaml:"acquisitionTimeout"`
	Idleness    Duration `yaml:"idlenessTimeout"`
}

type Config struct {
	LogLevel            string             `yaml:"logLevel"`
	HTTP                Endpoint           `yaml:"backgroundWorker"`
	Federator           Endpoint           `yaml:"federatorInternal"`
	Galley              Endpoint           `yaml:"galley"`
	Brig                Endpoint           `yaml:"brig"`
	Gundeck             Endpoint           `yaml:"gundeck"`
	Spar                Endpoint           `yaml:"spar"`
	Rabbit              RabbitConfig       `yaml:"rabbitmq"`
	Cassandra           CassandraConfig    `yaml:"cassandra"`
	CassandraBrig       CassandraConfig    `yaml:"cassandraBrig"`
	PostgresPool        PostgresPoolConfig `yaml:"postgresqlPool"`
	DefederationTimeout *int               `yaml:"defederationTimeout"`
	Pusher              struct {
		Min     int64 `yaml:"pushBackoffMinWait"`
		Max     int64 `yaml:"pushBackoffMaxWait"`
		Refresh int64 `yaml:"remotesRefreshInterval"`
	} `yaml:"backendNotificationPusher"`
	BackgroundJobs struct {
		Concurrency int      `yaml:"concurrency"`
		Timeout     Duration `yaml:"jobTimeout"`
		Attempts    int      `yaml:"maxAttempts"`
	} `yaml:"backgroundJobs"`
	Jobs struct {
		Poll            Duration  `yaml:"pollInterval"`
		Threads         int       `yaml:"workerThreads"`
		Visibility      Duration  `yaml:"visibilityTimeout"`
		JobHeartbeat    Duration  `yaml:"jobHeartbeatInterval"`
		WorkerHeartbeat Duration  `yaml:"workerHeartbeatInterval"`
		BackoffBase     float64   `yaml:"backoffBase"`
		BackoffCap      Duration  `yaml:"backoffCap"`
		Jitter          string    `yaml:"jitter"`
		Grace           *Duration `yaml:"gracefulShutdownTimeout"`
		ReaperInterval  Duration  `yaml:"reaperInterval"`
		ReaperTimeout   Duration  `yaml:"reaperTimeout"`
		WorkerStale     Duration  `yaml:"workerStaleThreshold"`
	} `yaml:"jobs"`
	MigrateConversations bool           `yaml:"migrateConversations"`
	MigrateCodes         bool           `yaml:"migrateConversationCodes"`
	MigrateTeamFeatures  bool           `yaml:"migrateTeamFeatures"`
	MigrateDomains       bool           `yaml:"migrateDomainRegistration"`
	MigrateUsers         bool           `yaml:"migrateUsers"`
	MigrateBlockList     bool           `yaml:"migrateBlockList"`
	MigratePasswordReset bool           `yaml:"migratePasswordReset"`
	Raw                  map[string]any `yaml:",inline"`
}

type GalleyConfig struct {
	Cassandra        CassandraConfig   `yaml:"cassandra"`
	Postgres         map[string]string `yaml:"postgresql"`
	PostgresPassword *string           `yaml:"postgresqlPassword"`
	Settings         struct {
		Domain string `yaml:"federationDomain"`
	} `yaml:"settings"`
	Raw map[string]any `yaml:",inline"`
}

func loadYAML(path string, value any) error {
	b, err := os.ReadFile(path)
	if err != nil || len(b) > 8<<20 {
		return errors.New("configuration could not be read")
	}
	if yaml.Unmarshal(b, value) != nil {
		return errors.New("invalid configuration")
	}
	return nil
}

func LoadConfig(workerPath, galleyPath string) (Config, GalleyConfig, error) {
	var c Config
	var g GalleyConfig
	// Arbiter defaults are source defaults; explicit null grace means unlimited.
	grace := Duration(30 * time.Second)
	c.Jobs.Poll = Duration(5 * time.Second)
	c.Jobs.Threads = 1
	c.Jobs.Visibility = Duration(60 * time.Second)
	c.Jobs.JobHeartbeat = Duration(30 * time.Second)
	c.Jobs.WorkerHeartbeat = Duration(10 * time.Second)
	c.Jobs.BackoffBase = 2
	c.Jobs.BackoffCap = Duration(24 * time.Hour)
	c.Jobs.Jitter = "equal"
	c.Jobs.Grace = &grace
	c.Jobs.ReaperInterval = Duration(300 * time.Second)
	c.Jobs.ReaperTimeout = Duration(300 * time.Second)
	c.Jobs.WorkerStale = Duration(300 * time.Second)
	if err := loadYAML(workerPath, &c); err != nil {
		return c, g, err
	}
	if err := loadYAML(galleyPath, &g); err != nil {
		return c, g, err
	}
	if !c.HTTP.valid() || !c.Federator.valid() || !c.Rabbit.Endpoint.valid() || !validDomain(g.Settings.Domain) || c.Rabbit.Insecure {
		return c, g, errors.New("invalid endpoint, domain or TLS configuration")
	}
	g.Settings.Domain = strings.ToLower(g.Settings.Domain)
	for _, store := range []CassandraConfig{c.Cassandra, c.CassandraBrig, g.Cassandra} {
		if !store.Endpoint.valid() || store.Keyspace == "" {
			return c, g, errors.New("invalid Cassandra configuration")
		}
	}
	if c.PostgresPool.Size < 1 || c.PostgresPool.Size > math.MaxInt32 || c.PostgresPool.Acquisition <= 0 || c.PostgresPool.Idleness <= 0 || len(g.Postgres) == 0 {
		return c, g, errors.New("invalid PostgreSQL pool configuration")
	}
	if c.Rabbit.AdminHost != "" && !(Endpoint{c.Rabbit.AdminHost, c.Rabbit.AdminPort}).valid() {
		return c, g, errors.New("invalid RabbitMQ admin endpoint")
	}
	if c.Pusher.Min <= 0 || c.Pusher.Max < c.Pusher.Min || c.Pusher.Max > int64((24*time.Hour)/time.Microsecond) || c.Pusher.Refresh <= 0 || c.Pusher.Refresh > int64((24*time.Hour)/time.Microsecond) {
		return c, g, errors.New("invalid notification retry configuration")
	}
	if c.Jobs.Threads < 1 || c.Jobs.Threads > 1000 || c.BackgroundJobs.Concurrency < 1 || c.BackgroundJobs.Concurrency > 1000 || c.BackgroundJobs.Attempts < 1 || c.BackgroundJobs.Attempts > 1000 || c.BackgroundJobs.Timeout <= 0 || c.Jobs.BackoffBase <= 0 || math.IsNaN(c.Jobs.BackoffBase) || math.IsInf(c.Jobs.BackoffBase, 0) || (c.Jobs.Jitter != "none" && c.Jobs.Jitter != "equal" && c.Jobs.Jitter != "full") {
		return c, g, errors.New("invalid worker configuration")
	}
	if c.DefederationTimeout != nil && (*c.DefederationTimeout <= 0 || *c.DefederationTimeout > 86400) {
		return c, g, errors.New("invalid defederation timeout")
	}
	return c, g, nil
}

func validDomain(domain string) bool {
	if len(domain) > 253 || net.ParseIP(domain) != nil {
		return false
	}
	labels := strings.Split(domain, ".")
	if len(labels) < 2 && !strings.EqualFold(domain, "localhost") {
		return false
	}
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-') {
				return false
			}
		}
	}
	return labels[len(labels)-1][0] < '0' || labels[len(labels)-1][0] > '9'
}

func verifiedTLS(host, ca string) (*tls.Config, error) {
	var roots *x509.CertPool
	if ca != "" {
		data, err := os.ReadFile(ca)
		if err != nil {
			return nil, errors.New("trusted CA could not be read")
		}
		roots = x509.NewCertPool()
		if !roots.AppendCertsFromPEM(data) {
			return nil, errors.New("invalid trusted CA")
		}
	}
	return &tls.Config{ServerName: host, RootCAs: roots, MinVersion: tls.VersionTLS12}, nil
}
