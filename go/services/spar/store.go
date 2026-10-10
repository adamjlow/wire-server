// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func contacts(ctx context.Context, c Config) ([]string, error) {
	if c.DiscoveryURL == "" {
		return []string{c.Cassandra.Endpoint.Host}, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.DiscoveryURL, nil)
	if err != nil {
		return nil, errors.New("invalid Cassandra discovery configuration")
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, errors.New("Cassandra discovery failed")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, errors.New("Cassandra discovery failed")
	}
	var result struct {
		Roles map[string][]struct {
			Address string `json:"privateIpAddress"`
		} `json:"roles"`
	}
	if json.NewDecoder(response.Body).Decode(&result) != nil {
		return nil, errors.New("invalid Cassandra discovery response")
	}
	var hosts []string
	for _, role := range []string{"cassandra_spar", "cassandra_spar_seed"} {
		for _, host := range result.Roles[role] {
			if host.Address != "" {
				hosts = append(hosts, host.Address)
			}
		}
	}
	if len(hosts) == 0 {
		return nil, errors.New("Cassandra discovery returned no contacts")
	}
	return hosts, nil
}
func openStore(ctx context.Context, c Config) (*gocql.Session, error) {
	hosts, err := contacts(ctx, c)
	if err != nil {
		return nil, err
	}
	cluster := gocql.NewCluster(hosts...)
	cluster.Port = c.Cassandra.Endpoint.Port
	cluster.Keyspace = c.Cassandra.Keyspace
	cluster.ProtoVersion = 4
	cluster.Consistency = gocql.LocalQuorum
	cluster.ConnectTimeout = 10 * time.Second
	cluster.Timeout = 10 * time.Second
	cluster.Logger = databaseLogger{}
	if c.Cassandra.Datacentre != "" {
		cluster.HostFilter = gocql.DataCentreHostFilter(c.Cassandra.Datacentre)
	}
	if c.Cassandra.CA != "" {
		pem, readErr := os.ReadFile(c.Cassandra.CA)
		if readErr != nil {
			return nil, errors.New("cannot read Cassandra CA")
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(pem) {
			return nil, errors.New("invalid Cassandra CA")
		}
		cluster.SslOpts = &gocql.SslOptions{Config: &tls.Config{RootCAs: roots, ServerName: c.Cassandra.Endpoint.Host, MinVersion: tls.VersionTLS12}, EnableHostVerification: true}
	}
	session, err := cluster.CreateSession()
	if err != nil {
		return nil, errors.New("Cassandra initialization failed")
	}
	var version int32
	err = session.Query("select version from meta where id=1 order by version desc limit 1").Consistency(gocql.One).WithContext(ctx).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5}).Scan(&version)
	if err != nil || version < 23 {
		session.Close()
		return nil, errors.New("Spar Cassandra schema must be at least version 23")
	}
	return session, nil
}

// Driver fields can contain query details. Service boundaries report useful
// generic failures without recording authentication or provisioning payloads.
type databaseLogger struct{}

func (databaseLogger) Error(string, ...gocql.LogField)   { log.Print("Cassandra operation failed") }
func (databaseLogger) Warning(string, ...gocql.LogField) { log.Print("Cassandra warning") }
func (databaseLogger) Info(string, ...gocql.LogField)    {}
func (databaseLogger) Debug(string, ...gocql.LogField)   {}
