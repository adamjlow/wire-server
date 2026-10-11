// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
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

type CassandraConfig struct {
	Endpoint   Endpoint `yaml:"endpoint"`
	Keyspace   string   `yaml:"keyspace"`
	TLSCA      string   `yaml:"tlsCa"`
	Datacentre string   `yaml:"filterNodesByDatacentre"`
}
type Duration time.Duration

func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	v, err := time.ParseDuration(n.Value)
	if err != nil || v <= 0 {
		return errors.New("invalid positive duration")
	}
	*d = Duration(v)
	return nil
}

type PostgresPoolConfig struct {
	Size        int      `yaml:"size"`
	Acquisition Duration `yaml:"acquisitionTimeout"`
	Idleness    Duration `yaml:"idlenessTimeout"`
}
type Config struct {
	RandomPrekeys    *bool                 `yaml:"randomPrekeys"`
	InternalEvents   *internalEventsConfig `yaml:"internalEvents"`
	AWS              *brigAWSConfig        `yaml:"aws"`
	Elasticsearch    *searchIndexConfig    `yaml:"elasticsearch"`
	PostgresPool     PostgresPoolConfig    `yaml:"postgresqlPool"`
	Brig             Endpoint              `yaml:"brig"`
	Cargohold        Endpoint              `yaml:"cargohold"`
	Galley           Endpoint              `yaml:"galley"`
	Spar             Endpoint              `yaml:"spar"`
	Gundeck          Endpoint              `yaml:"gundeck"`
	Federator        *Endpoint             `yaml:"federatorInternal"`
	Rabbit           *brigRabbitConfig     `yaml:"rabbitmq"`
	Enterprise       *Endpoint             `yaml:"wireServerEnterprise"`
	Cassandra        CassandraConfig       `yaml:"cassandra"`
	Postgres         map[string]string     `yaml:"postgresql"`
	PostgresPassword *string               `yaml:"postgresqlPassword"`
	Migration        map[string]string     `yaml:"postgresMigration"`
	ZAuth            struct {
		PrivateKeys string `yaml:"privateKeys"`
		PublicKeys  string `yaml:"publicKeys"`
		Settings    struct {
			KeyIndex           int   `yaml:"keyIndex"`
			UserTTL            int64 `yaml:"userTokenTimeout"`
			SessionTTL         int64 `yaml:"sessionTokenTimeout"`
			AccessTTL          int64 `yaml:"accessTokenTimeout"`
			ProviderTTL        int64 `yaml:"providerTokenTimeout"`
			LegalHoldUserTTL   int64 `yaml:"legalHoldUserTokenTimeout"`
			LegalHoldAccessTTL int64 `yaml:"legalHoldAccessTokenTimeout"`
		} `yaml:"authSettings"`
	} `yaml:"zauth"`
	Settings struct {
		EmailVisibility           string                    `yaml:"setEmailVisibility"`
		UserMaxConnections        int64                     `yaml:"setUserMaxConnections"`
		EnableMLS                 bool                      `yaml:"setEnableMLS"`
		KeyPackageMaximumLifetime *mlsLifetimeMaximum       `yaml:"setKeyPackageMaximumLifetime"`
		MaxConversationSize       *uint16                   `yaml:"setMaxConvSize"`
		MaxPermanentClients       *int                      `yaml:"setUserMaxPermClients"`
		ConsumableNotifications   bool                      `yaml:"setConsumableNotifications"`
		PropertyMaxKeyLen         *int64                    `yaml:"setPropertyMaxKeyLen"`
		PropertyMaxValueLen       *int64                    `yaml:"setPropertyMaxValueLen"`
		FederationStrategy        *string                   `yaml:"setFederationStrategy"`
		FederationDomains         *[]federationStaticDomain `yaml:"setFederationDomainConfigs"`
		SQSThrottleMillis         *int64                    `yaml:"sqsThrottleMillis"`
		RestrictUserCreation      *bool                     `yaml:"setRestrictUserCreation"`
		EphemeralCreation         *bool                     `yaml:"setEphemeralUserCreationEnabled"`
		AllowlistDomains          *[]string                 `yaml:"setAllowlistEmailDomains"`
		MaxTeamSize               *uint32                   `yaml:"setMaxTeamSize"`
		ActivationTimeout         *float64                  `yaml:"setActivationTimeout"`
		CookieInsecure            bool                      `yaml:"setCookieInsecure"`
		CookieRenewAge            *int64                    `yaml:"setUserCookieRenewAge"`
		CookieLimit               *int                      `yaml:"setUserCookieLimit"`
		CookieThrottle            *struct {
			StdDev     float64 `yaml:"stdDev"`
			RetryAfter int64   `yaml:"retryAfter"`
		} `yaml:"setUserCookieThrottle"`
		FailedLogins    *failedLoginLimit `yaml:"setLimitFailedLogins"`
		SuspendInactive *struct {
			Timeout float64 `yaml:"suspendTimeout"`
		} `yaml:"setSuspendInactiveUsers"`
		PasswordPolicy   *passwordPolicy     `yaml:"setPasswordHashingOptions"`
		PasswordRate     *passwordRateConfig `yaml:"setPasswordHashingRateLimit"`
		Domain           string              `yaml:"setFederationDomain"`
		DefaultLocale    string              `yaml:"setDefaultUserLocale"`
		DisabledVersions []string            `yaml:"setDisabledAPIVersions"`
		FeatureFlags     *struct {
			ConferenceCalling struct {
				DefaultForNew *struct {
					Status string `yaml:"status"`
				} `yaml:"defaultForNew"`
				DefaultForNull *struct {
					Status string `yaml:"status"`
				} `yaml:"defaultForNull"`
			} `yaml:"conferenceCalling"`
		} `yaml:"setFeatureFlags"`
		Other map[string]any `yaml:",inline"`
	} `yaml:"optSettings"`
	// Preserve options not yet typed in this initial implementation slice.
	// This is not a claim that all retained options are implemented.
	Other map[string]any `yaml:",inline"`
}

func LoadConfig(path string) (Config, error) {
	var c Config
	data, err := os.ReadFile(path)
	if err != nil {
		return c, errors.New("cannot read Brig configuration")
	}
	if yaml.Unmarshal(data, &c) != nil {
		return c, errors.New("invalid Brig configuration")
	}
	if err = c.Validate(); err != nil {
		return c, err
	}
	return c, nil
}
func (c Config) Validate() error {
	invalid := errors.New("invalid Brig configuration")
	if _, err := federationReadFromConfig(c); err != nil {
		return invalid
	}
	for _, e := range []Endpoint{c.Brig, c.Cargohold, c.Galley, c.Spar, c.Gundeck, c.Cassandra.Endpoint} {
		if !e.valid() {
			return invalid
		}
	}
	if c.Cassandra.Keyspace == "" || c.Postgres == nil || c.Settings.Domain == "" || c.ZAuth.PrivateKeys == "" || c.ZAuth.PublicKeys == "" || c.ZAuth.Settings.KeyIndex < 1 {
		return invalid
	}
	for _, e := range []*Endpoint{c.Federator, c.Enterprise} {
		if e != nil && !e.valid() {
			return invalid
		}
	}
	for _, mode := range c.Migration {
		if mode != "cassandra" && mode != "postgresql" && mode != "migration-to-postgresql" {
			return invalid
		}
	}
	if _, err := conferenceCallingStatus(c, nil); err != nil {
		return invalid
	}
	if _, err := conferenceCallingNew(c); err != nil {
		return invalid
	}
	if c.Settings.DefaultLocale != "" {
		if _, err := parseRegistrationLocale(c.Settings.DefaultLocale); err != nil {
			return invalid
		}
	}
	if c.Settings.ActivationTimeout != nil {
		if _, err := cassandraTTL(*c.Settings.ActivationTimeout); err != nil {
			return invalid
		}
	}
	return nil
}

func readSigningKeys(path string, size int) ([][]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("cannot read ZAuth keys")
	}
	var keys [][]byte
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		if strings.ContainsAny(line, "\r\n") {
			return nil, errors.New("invalid ZAuth key encoding")
		}
		// Sodium.Crypto.Sign's Read instances use Base64.URL for both keys.
		key, err := base64.URLEncoding.DecodeString(line)
		if err != nil || len(key) != size {
			return nil, errors.New("invalid ZAuth key encoding")
		}
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return nil, errors.New("empty ZAuth key file")
	}
	return keys, nil
}

func (c Config) signingKeys() (ed25519.PrivateKey, []ed25519.PublicKey, error) {
	private, err := readSigningKeys(c.ZAuth.PrivateKeys, ed25519.PrivateKeySize)
	if err != nil {
		return nil, nil, err
	}
	public, err := readSigningKeys(c.ZAuth.PublicKeys, ed25519.PublicKeySize)
	if err != nil {
		return nil, nil, err
	}
	if c.ZAuth.Settings.KeyIndex > len(private) {
		return nil, nil, errors.New("invalid ZAuth signing key index")
	}
	keys := make([]ed25519.PublicKey, len(public))
	for i, key := range public {
		keys[i] = ed25519.PublicKey(key)
	}
	return ed25519.PrivateKey(private[c.ZAuth.Settings.KeyIndex-1]), keys, nil
}
