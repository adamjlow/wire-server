// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
	"go.yaml.in/yaml/v3"
)

type searchIndexConfig struct {
	URL                   string  `yaml:"url"`
	Index                 string  `yaml:"index"`
	AdditionalIndex       *string `yaml:"additionalWriteIndex"`
	AdditionalURL         *string `yaml:"additionalWriteIndexUrl"`
	Credentials           *string `yaml:"credentials"`
	AdditionalCredentials *string `yaml:"additionalCredentials"`
	SkipVerify            bool    `yaml:"insecureSkipVerifyTls"`
	CA                    *string `yaml:"caCert"`
	AdditionalSkipVerify  bool    `yaml:"additionalInsecureSkipVerifyTls"`
	AdditionalCA          *string `yaml:"additionalCaCert"`
}

type searchIndexConnection struct {
	base               *url.URL
	index              string
	client             *http.Client
	username, password *string
}

func newSearchConnection(endpoint, index string, credentials, ca *string, skipVerify bool) (searchIndexConnection, error) {
	invalid := errors.New("invalid search index configuration")
	base, err := url.Parse(endpoint)
	if err != nil || base.Host == "" || base.Scheme != "http" && base.Scheme != "https" || index == "" || base.RawQuery != "" || base.Fragment != "" {
		return searchIndexConnection{}, invalid
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// This explicit existing native option is preserved, never enabled by
	// default. Its inherited trust risk is recorded in the migration plan.
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: skipVerify}
	if ca != nil {
		pem, err := os.ReadFile(*ca)
		if err != nil {
			return searchIndexConnection{}, invalid
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(pem) {
			return searchIndexConnection{}, invalid
		}
		transport.TLSClientConfig.RootCAs = roots
	}
	conn := searchIndexConnection{base: base, index: index, client: &http.Client{Transport: transport, Timeout: 30 * time.Second}}
	if credentials != nil {
		data, err := os.ReadFile(*credentials)
		if err != nil {
			return searchIndexConnection{}, invalid
		}
		var secret struct{ Username, Password *string }
		if yaml.Unmarshal(data, &secret) != nil || secret.Username == nil || secret.Password == nil {
			return searchIndexConnection{}, invalid
		}
		conn.username, conn.password = secret.Username, secret.Password
	}
	return conn, nil
}

func newSearchIndex(config searchIndexConfig) (*searchIndexWriter, error) {
	main, err := newSearchConnection(config.URL, config.Index, config.Credentials, config.CA, config.SkipVerify)
	if err != nil {
		return nil, err
	}
	writer := &searchIndexWriter{connections: []searchIndexConnection{main}}
	if config.AdditionalIndex != nil {
		additional := main
		additional.index = *config.AdditionalIndex
		if additional.index == "" {
			writer.close()
			return nil, errors.New("invalid additional search index")
		}
		if config.AdditionalURL != nil {
			additional, err = newSearchConnection(*config.AdditionalURL, *config.AdditionalIndex, config.AdditionalCredentials, config.AdditionalCA, config.AdditionalSkipVerify)
			if err != nil {
				writer.close()
				return nil, err
			}
		}
		writer.connections = append(writer.connections, additional)
	}
	return writer, nil
}

type searchIndexWriter struct{ connections []searchIndexConnection }

func (s *searchIndexWriter) close() {
	for _, conn := range s.connections {
		conn.client.CloseIdleConnections()
	}
}

func (s *searchIndexWriter) upsert(ctx context.Context, doc indexDocument, version *int64, requestID string) error {
	body, err := json.Marshal(doc)
	if err != nil {
		return errors.New("search document encoding failed")
	}
	if len(s.connections) == 0 {
		return errors.New("search index unavailable")
	}
	for _, conn := range s.connections {
		endpoint := conn.base.JoinPath(conn.index, "user", doc.ID)
		if version != nil {
			endpoint.RawQuery = url.Values{"version": {strconv.FormatInt(*version, 10)}, "version_type": {"external_gt"}}.Encode()
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint.String(), bytes.NewReader(body))
		if err != nil {
			return errors.New("invalid search index endpoint")
		}
		req.Header.Set("Content-Type", "application/json")
		if requestID != "" {
			req.Header.Set("Request-Id", requestID)
		}
		if conn.username != nil {
			req.SetBasicAuth(*conn.username, *conn.password)
		}
		response, err := conn.client.Do(req)
		if err != nil {
			return errors.New("search index update failed")
		}
		_, drainErr := io.Copy(io.Discard, io.LimitReader(response.Body, 4<<20))
		_ = response.Body.Close()
		if drainErr != nil || response.StatusCode < 200 || response.StatusCode >= 300 && response.StatusCode != http.StatusConflict {
			return errors.New("search index update failed")
		}
	}
	return nil
}

type searchIndexService struct {
	users interface {
		indexUser(context.Context, gocql.UUID) (*indexUser, error)
	}
	team   func(context.Context, string, string, string) (string, *indexRole, error)
	writer *searchIndexWriter
}

func (s searchIndexService) sync(ctx context.Context, user gocql.UUID, requestID string) error {
	r, err := s.users.indexUser(ctx, user)
	if err != nil {
		return err
	}
	if r == nil {
		return s.writer.upsert(ctx, indexDocument{ID: user.String()}, nil, requestID)
	}
	visibility := "searchable-by-own-team"
	var role *indexRole
	if r.user.Team != nil {
		visibility, role, err = s.team(ctx, r.user.Team.String(), user.String(), requestID)
		if err != nil {
			return err
		}
	}
	doc, err := r.document(visibility, role)
	if err != nil {
		return err
	}
	version := indexDocumentVersion(r.updated, role)
	return s.writer.upsert(ctx, doc, &version, requestID)
}
