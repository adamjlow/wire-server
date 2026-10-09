// SPDX-License-Identifier: AGPL-3.0-only
package federator

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"go.yaml.in/yaml/v3"
	"net"
	"os"
	"strconv"
	"time"
)

type Endpoint struct {
	Host string `yaml:"host"`
	Port uint16 `yaml:"port"`
}

func (e Endpoint) address() string { return net.JoinHostPort(e.Host, strconv.Itoa(int(e.Port))) }

type Settings struct {
	UseSystemCAStore     *bool   `yaml:"useSystemCAStore"`
	RemoteCAStore        *string `yaml:"remoteCAStore"`
	ClientCertificate    string  `yaml:"clientCertificate"`
	ClientPrivateKey     string  `yaml:"clientPrivateKey"`
	TCPConnectionTimeout *int64  `yaml:"tcpConnectionTimeout"`
	DNSHost              *string `yaml:"dnsHost"`
	DNSPort              *uint16 `yaml:"dnsPort"`
}
type Config struct {
	Internal      Endpoint `yaml:"federatorInternal"`
	External      Endpoint `yaml:"federatorExternal"`
	Brig          Endpoint `yaml:"brig"`
	Galley        Endpoint `yaml:"galley"`
	Cargohold     Endpoint `yaml:"cargohold"`
	LogLevel      string   `yaml:"logLevel"`
	LogNetStrings *bool    `yaml:"logNetStrings"`
	LogFormat     *string  `yaml:"logFormat"`
	Settings      Settings `yaml:"optSettings"`
}

func ReadConfig(path string) (Config, error) {
	var c Config
	data, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	if err = yaml.Unmarshal(data, &c); err != nil {
		return c, err
	}
	for _, e := range []Endpoint{c.Internal, c.External, c.Brig, c.Galley, c.Cargohold} {
		if e.Host == "" || e.Port == 0 {
			return c, errors.New("required endpoint missing")
		}
	}
	if c.Settings.UseSystemCAStore == nil || c.Settings.TCPConnectionTimeout == nil || *c.Settings.TCPConnectionTimeout <= 0 || *c.Settings.TCPConnectionTimeout > int64((24*time.Hour)/time.Microsecond) || c.Settings.ClientCertificate == "" || c.Settings.ClientPrivateKey == "" {
		return c, errors.New("required TLS or connection setting missing or invalid")
	}
	if c.LogLevel == "" {
		return c, errors.New("required logLevel missing")
	}
	switch c.LogLevel {
	case "Trace", "Debug", "Info", "Warn", "Error", "Fatal":
	default:
		return c, errors.New("invalid logLevel")
	}
	return c, nil
}
func (s Settings) tlsConfig() (*tls.Config, [32]byte, error) {
	var sum [32]byte
	cert, err := os.ReadFile(s.ClientCertificate)
	if err != nil {
		return nil, sum, err
	}
	key, err := os.ReadFile(s.ClientPrivateKey)
	if err != nil {
		return nil, sum, err
	}
	pair, err := tls.X509KeyPair(cert, key)
	if err != nil {
		return nil, sum, err
	}
	roots := x509.NewCertPool()
	if s.UseSystemCAStore != nil && *s.UseSystemCAStore {
		roots, err = x509.SystemCertPool()
		if err != nil {
			return nil, sum, err
		}
	}
	ca := []byte{}
	if s.RemoteCAStore != nil {
		ca, err = os.ReadFile(*s.RemoteCAStore)
		if err != nil {
			return nil, sum, err
		}
		if !roots.AppendCertsFromPEM(ca) {
			return nil, sum, errors.New("invalid CA store")
		}
	}
	h := sha256.New()
	h.Write(cert)
	h.Write(key)
	h.Write(ca)
	copy(sum[:], h.Sum(nil))
	return &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, Certificates: []tls.Certificate{pair}, NextProtos: []string{"h2"}, CipherSuites: []uint16{tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384, tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384}}, sum, nil
}
func (s Settings) dnsAddress() string {
	if s.DNSHost != nil {
		port := uint16(53)
		if s.DNSPort != nil {
			port = *s.DNSPort
		}
		return net.JoinHostPort(*s.DNSHost, fmt.Sprint(port))
	}
	return ""
}
