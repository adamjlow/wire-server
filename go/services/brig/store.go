// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"log"
	"math"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.yaml.in/yaml/v3"
)

// Driver diagnostics can include credentials or personal rows. Keep service
// errors generic and never print the underlying driver text.
type databaseLogger struct{}

func (databaseLogger) Error(string, ...gocql.LogField)   { log.Print("Cassandra operation failed") }
func (databaseLogger) Warning(string, ...gocql.LogField) { log.Print("Cassandra warning") }
func (databaseLogger) Info(string, ...gocql.LogField)    {}
func (databaseLogger) Debug(string, ...gocql.LogField)   {}

func openCassandra(ctx context.Context, c Config) (*gocql.Session, error) {
	cluster := gocql.NewCluster(c.Cassandra.Endpoint.Host)
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
	if c.Cassandra.TLSCA != "" {
		pem, err := os.ReadFile(c.Cassandra.TLSCA)
		if err != nil {
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
	err = session.Query("SELECT version FROM meta WHERE id=1 ORDER BY version DESC LIMIT 1").Consistency(gocql.One).WithContext(ctx).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5}).Scan(&version)
	if err != nil || version < 94 {
		session.Close()
		return nil, errors.New("Brig Cassandra schema must be at least version 94")
	}
	return session, nil
}

// Same reviewed libpq escaping used by the Go background worker. Values cannot
// escape their option, and secret-bearing parser/connection errors stay private.
func postgresConfig(c Config) (*pgxpool.Config, error) {
	invalid := errors.New("invalid PostgreSQL configuration")
	if c.PostgresPool.Size < 1 || c.PostgresPool.Size > math.MaxInt32 || c.PostgresPool.Acquisition <= 0 || c.PostgresPool.Idleness <= 0 || len(c.Postgres) == 0 {
		return nil, invalid
	}
	params := make(map[string]string, len(c.Postgres))
	for k, v := range c.Postgres {
		if k == "" || strings.ContainsAny(k, "= \\'\t\n\r\x00") || strings.ContainsRune(v, '\x00') {
			return nil, invalid
		}
		params[k] = v
	}
	if c.PostgresPassword != nil {
		data, err := os.ReadFile(*c.PostgresPassword)
		if err != nil {
			return nil, errors.New("cannot read PostgreSQL password file")
		}
		var password string
		if yaml.Unmarshal(data, &password) != nil || strings.ContainsRune(password, '\x00') {
			return nil, errors.New("invalid PostgreSQL password file")
		}
		params["password"] = password
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		v := strings.ReplaceAll(strings.ReplaceAll(params[k], `\`, `\\`), `'`, `\'`)
		parts = append(parts, k+"='"+v+"'")
	}
	config, err := pgxpool.ParseConfig(strings.Join(parts, " "))
	if err != nil {
		return nil, invalid
	}
	config.MaxConns = int32(c.PostgresPool.Size)
	config.MinConns = 0
	config.MaxConnIdleTime = time.Duration(c.PostgresPool.Idleness)
	config.MaxConnLifetime = 0
	return config, nil
}

func openPostgres(ctx context.Context, c Config) (*pgxpool.Pool, error) {
	config, err := postgresConfig(c)
	if err != nil {
		return nil, err
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, errors.New("PostgreSQL initialization failed")
	}
	probe, cancel := context.WithTimeout(ctx, time.Duration(c.PostgresPool.Acquisition))
	defer cancel()
	if pool.Ping(probe) != nil {
		pool.Close()
		return nil, errors.New("PostgreSQL connection failed")
	}
	return pool, nil
}
