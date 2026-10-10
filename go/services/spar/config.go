// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"encoding/hex"
	"errors"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"unicode"

	"go.yaml.in/yaml/v3"
)

type Endpoint struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

func (e Endpoint) Address() string { return net.JoinHostPort(e.Host, strconv.Itoa(e.Port)) }
func (e Endpoint) valid() bool     { return e.Host != "" && e.Port > 0 && e.Port <= 65535 }

type DomainConfig struct {
	AppURI   string           `yaml:"spAppUri"`
	SSOURI   string           `yaml:"spSsoUri"`
	Contacts []map[string]any `yaml:"contacts"`
}
type SAMLConfig struct {
	Host         string `yaml:"spHost"`
	Port         int    `yaml:"spPort"`
	LogLevel     string `yaml:"logLevel"`
	DomainConfig `yaml:",inline"`
	Domains      map[string]DomainConfig `yaml:"spDomainConfigs"`
}
type CassandraConfig struct {
	Endpoint   Endpoint `yaml:"endpoint"`
	Keyspace   string   `yaml:"keyspace"`
	CA         string   `yaml:"tlsCa"`
	Datacentre string   `yaml:"filterNodesByDatacentre"`
}
type Config struct {
	SAML                 SAMLConfig      `yaml:"saml"`
	Brig                 Endpoint        `yaml:"brig"`
	Galley               Endpoint        `yaml:"galley"`
	Cassandra            CassandraConfig `yaml:"cassandra"`
	MaxAuthRequestTTL    *TTL            `yaml:"maxttlAuthreq"`
	MaxAssertionTTL      *TTL            `yaml:"maxttlAuthresp"`
	MaxTokens            *int            `yaml:"maxScimTokens"`
	RichInfoLimit        *int            `yaml:"richInfoLimit"`
	DisabledVersions     []string        `yaml:"disabledAPIVersions"`
	SCIMBaseURI          string          `yaml:"scimBaseUri"`
	EnableEmailDiscovery *bool           `yaml:"enableIdPByEmailDiscovery"`
	Fingerprints         []string        `yaml:"idpCertFingerprintAllowlist"`
	DiscoveryURL         string          `yaml:"discoUrl"`
	LogNetStrings        *bool           `yaml:"logNetStrings"`
	LogFormat            string          `yaml:"logFormat"`
}

func absoluteURI(s string) bool { u, err := url.Parse(s); return err == nil && u.IsAbs() }
func validDomain(c DomainConfig) bool {
	return absoluteURI(c.AppURI) && absoluteURI(c.SSOURI) && c.Contacts != nil
}
func knownVersion(s string) bool {
	if len(s) < 2 || s[0] != 'v' {
		return false
	}
	n, err := strconv.Atoi(s[1:])
	return err == nil && n >= 0 && n <= 19 && s == "v"+strconv.Itoa(n)
}
func (c Config) Validate() error {
	invalid := errors.New("invalid Spar configuration")
	if !(Endpoint{c.SAML.Host, c.SAML.Port}).valid() || !c.Brig.valid() || !c.Galley.valid() || !c.Cassandra.Endpoint.valid() || c.Cassandra.Keyspace == "" || !absoluteURI(c.SCIMBaseURI) {
		return invalid
	}
	if c.SAML.LogLevel == "" || c.MaxAuthRequestTTL == nil || c.MaxAssertionTTL == nil || c.MaxTokens == nil || c.RichInfoLimit == nil || c.EnableEmailDiscovery == nil || c.DisabledVersions == nil {
		return invalid
	}
	if c.SAML.Domains == nil {
		if !validDomain(c.SAML.DomainConfig) {
			return invalid
		}
	} else {
		if c.SAML.AppURI != "" || c.SAML.SSOURI != "" || c.SAML.Contacts != nil {
			return invalid
		}
		for host, d := range c.SAML.Domains {
			if host == "" || !validDomain(d) {
				return invalid
			}
		}
	}
	for _, v := range c.DisabledVersions {
		if v != "development" && !knownVersion(v) {
			return invalid
		}
	}
	for _, fingerprint := range c.Fingerprints {
		cleaned := strings.Map(func(r rune) rune {
			if r == ':' || unicode.IsSpace(r) {
				return -1
			}
			return r
		}, fingerprint)
		raw, err := hex.DecodeString(cleaned)
		if err != nil || len(raw) != 20 {
			return invalid
		}
	}
	if c.DiscoveryURL != "" && !absoluteURI(c.DiscoveryURL) {
		return invalid
	}
	return nil
}
func LoadConfig(path string) (Config, error) {
	var c Config
	data, err := os.ReadFile(path)
	if err != nil {
		return c, errors.New("cannot read Spar configuration")
	}
	if err = yaml.Unmarshal(data, &c); err != nil {
		return c, errors.New("invalid Spar configuration")
	}
	return c, c.Validate()
}
