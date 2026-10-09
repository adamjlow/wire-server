// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type botService struct {
	URL          string
	Token        string
	Fingerprints [][]byte
	Enabled      bool
}

type botServiceStore interface {
	Service(context.Context, string, string) (*botService, error)
}

type cassandraBotStore struct{ Session *gocql.Session }

func (s cassandraBotStore) Service(ctx context.Context, provider, service string) (*botService, error) {
	pid, err := parseUserID(provider)
	if err != nil {
		return nil, errors.New("invalid bot service identifier")
	}
	sid, err := parseUserID(service)
	if err != nil {
		return nil, errors.New("invalid bot service identifier")
	}
	values := nullableCassandraRow("base_url", "auth_token", "fingerprints", "enabled")
	err = s.Session.Query(`SELECT base_url,auth_token,fingerprints,enabled FROM service WHERE provider=? AND id=?`, pid, sid).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(cassandraRetry(1)).MapScan(values)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.New("bot service query failed")
	}
	unwrapCassandraRow(values)
	u, urlOK := values["base_url"].(string)
	token, tokenOK := values["auth_token"].(string)
	pins, pinsOK := values["fingerprints"].([][]byte)
	enabled, enabledOK := values["enabled"].(bool)
	if !urlOK || !tokenOK || !pinsOK || !enabledOK {
		return nil, errors.New("invalid stored bot service")
	}
	return &botService{URL: u, Token: token, Fingerprints: pins, Enabled: enabled}, nil
}

func unsignedMPI(n *big.Int) []byte {
	value := n.Bytes()
	if len(value) > 0 && value[0]&0x80 != 0 {
		value = append([]byte{0}, value...)
	}
	encoded := make([]byte, 4+len(value))
	binary.BigEndian.PutUint32(encoded, uint32(len(value)))
	copy(encoded[4:], value)
	return encoded
}

// Ssl.Util.rsaFingerprint hashes decimal RSA_size followed by BN_bn2mpi(N)
// and BN_bn2mpi(E). This is deliberately not an SPKI/certificate fingerprint.
func botRSAFingerprint(key *rsa.PublicKey) []byte {
	value := []byte(strconv.Itoa(key.Size()))
	value = append(value, unsignedMPI(key.N)...)
	value = append(value, unsignedMPI(big.NewInt(int64(key.E)))...)
	sum := sha256.Sum256(value)
	return sum[:]
}

type botPinError struct{}

func (botPinError) Error() string { return "bot certificate authentication failed" }

func pinnedBotTLS(pins [][]byte, roots *x509.CertPool) *tls.Config {
	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		CipherSuites: []uint16{tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384, tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256, tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256},
		// Delegate the built-in hostname/CA-only decision to the mandatory
		// verifier below, because native registered pins also authenticate
		// self-signed certificates. Every connection still verifies its pin,
		// signature and dates; no unchecked TLS client is exposed.
		InsecureSkipVerify: true,
		VerifyConnection: func(state tls.ConnectionState) error {
			if len(state.PeerCertificates) == 0 {
				return botPinError{}
			}
			leaf := state.PeerCertificates[0]
			key, ok := leaf.PublicKey.(*rsa.PublicKey)
			if !ok || key.N == nil || key.N.Sign() <= 0 || key.E <= 0 {
				return botPinError{}
			}
			fingerprint := botRSAFingerprint(key)
			matched := 0
			for _, pin := range pins {
				matched |= subtle.ConstantTimeCompare(fingerprint, pin)
			}
			if matched != 1 {
				return botPinError{}
			}
			intermediates := x509.NewCertPool()
			for _, cert := range state.PeerCertificates[1:] {
				intermediates.AddCert(cert)
			}
			now := time.Now()
			if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, Intermediates: intermediates, CurrentTime: now}); err == nil {
				return nil
			}
			// Native OpenSSL X509_verify checks the leaf's own signature rather
			// than requiring CA basic constraints on self-signed service leaves.
			if now.Before(leaf.NotBefore) || now.After(leaf.NotAfter) || leaf.CheckSignature(leaf.SignatureAlgorithm, leaf.RawTBSCertificate, leaf.Signature) != nil {
				return botPinError{}
			}
			return nil
		},
	}
}

type externalBotDelivery struct {
	Context context.Context
	Store   botServiceStore
	Roots   *x509.CertPool
	slots   chan struct{}
	wg      sync.WaitGroup
}

func newExternalBotDelivery(ctx context.Context, store botServiceStore) *externalBotDelivery {
	return &externalBotDelivery{Context: ctx, Store: store, slots: make(chan struct{}, 100)}
}

func (d *externalBotDelivery) DeliverAsync(_ context.Context, bots []localConversationMember, event map[string]any) {
	body, err := json.Marshal(event)
	if err != nil {
		return
	}
	// The job attempt context ends after ACK. Native FireAndForget callbacks
	// survive that attempt and belong to the process's independent lifetime.
	d.wg.Go(func() {
		for _, bot := range bots {
			select {
			case <-d.Context.Done():
				return
			case d.slots <- struct{}{}:
			}
			d.wg.Go(func() {
				defer func() { <-d.slots }()
				_ = d.deliver(d.Context, bot, body)
			})
		}
	})
}

func (d *externalBotDelivery) deliver(ctx context.Context, bot localConversationMember, body []byte) error {
	if !bot.bot() {
		return errors.New("invalid bot recipient")
	}
	service, err := d.Store.Service(ctx, *bot.Provider, *bot.Service)
	if err != nil {
		return err
	}
	if service == nil || !service.Enabled {
		return nil // Native missing/disabled services do not receive callbacks.
	}
	u, err := url.Parse(service.URL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || strings.ContainsAny(service.Token, "\r\n") {
		return errors.New("invalid bot service endpoint")
	}
	u.User, u.RawQuery, u.Fragment = nil, "", ""
	u.Path = strings.TrimSuffix(u.Path, "/") + "/bots/" + bot.ID + "/messages"
	u.RawPath = ""
	transport := &http.Transport{Proxy: nil, TLSClientConfig: pinnedBotTLS(service.Fingerprints, d.Roots)}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for attempt := 0; ; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		req, err := http.NewRequestWithContext(ctx, "POST", u.String(), bytes.NewReader(body))
		if err != nil {
			return errors.New("invalid bot callback request")
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+service.Token)
		response, err := client.Do(req)
		retry := err != nil
		var pinError botPinError
		if errors.As(err, &pinError) {
			retry = false
		}
		if response != nil {
			response.Body.Close()
			if response.StatusCode >= 200 && response.StatusCode < 300 {
				return nil
			}
			retry = response.StatusCode >= 500 && response.StatusCode < 600
		}
		// Bilge.Retry retries connection/timeout/internal failures and 5xx,
		// but not 4xx, redirects or explicit pin-verification exceptions.
		if !retry || attempt == 3 {
			return errors.New("bot callback failed")
		}
		if !wait(ctx, time.Second) {
			return ctx.Err()
		}
	}
}
