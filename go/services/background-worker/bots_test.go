// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestBotRSAFingerprintMatchesNativeOpenSSLPrimitive(t *testing.T) {
	// Public fixture from Testlib.MockIntegrationService. Expected digest was
	// independently computed with libcrypto BN_bn2mpi, not this implementation.
	const publicKey = `-----BEGIN PUBLIC KEY-----
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAu+Kg/PHHU3atXrUbKnw0
G06FliXcNt3lMwl2os5twEDcPPFw/feGiAKymxp+7JqZDrseS5D9THGrW+OQRIPH
WvUBdiLfGrZqJO223DB6D8K2Su/odmnjZJ2z23rhXoEArTplu+Dg9K+c2LVeXTKV
VPOaOzgtAB21XKRiQ4ermqgi3/njr03rXyq/qNkuNd6tNcg+HAfGxfGvvCSYBfiS
bUKr/BeArYRcjzr/h5m1In6fG/if9GEI6m8dxHT9JbY53wiksowy6ajCuqskIFg8
7X883H+LA/d6X5CTiPv1VMxXdBUiGPuC9IT/6CNQ1/LFt0P37ax58+LGYlaFo7la
nQIDAQAB
-----END PUBLIC KEY-----`
	block, _ := pem.Decode([]byte(publicKey))
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(botRSAFingerprint(key.(*rsa.PublicKey))); got != "8a8cb719e22381046ca1b7f610a18edcef26abf1687c5c4746acb44f8111219f" {
		t.Fatalf("native RSA fingerprint mismatch: %s", got)
	}
}

func syntheticBotCertificate(t *testing.T, expired bool) (tls.Certificate, *x509.Certificate) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	if expired {
		template.NotAfter = now.Add(-time.Minute)
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, leaf
}

func TestBotTLSRequiresPinSignatureAndDates(t *testing.T) {
	_, valid := syntheticBotCertificate(t, false)
	_, expired := syntheticBotCertificate(t, true)
	tampered := *valid
	tampered.Signature = append([]byte(nil), valid.Signature...)
	tampered.Signature[0] ^= 1
	for _, test := range []struct {
		name string
		leaf *x509.Certificate
		pin  []byte
		pass bool
	}{
		{"pinned-self-signed", valid, botRSAFingerprint(valid.PublicKey.(*rsa.PublicKey)), true},
		{"wrong-pin", valid, make([]byte, 32), false},
		{"expired", expired, botRSAFingerprint(expired.PublicKey.(*rsa.PublicKey)), false},
		{"tampered-signature", &tampered, botRSAFingerprint(valid.PublicKey.(*rsa.PublicKey)), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := pinnedBotTLS([][]byte{test.pin}, x509.NewCertPool())
			err := config.VerifyConnection(tls.ConnectionState{PeerCertificates: []*x509.Certificate{test.leaf}})
			if (err == nil) != test.pass {
				t.Fatal("certificate authentication result changed")
			}
		})
	}
	if err := pinnedBotTLS(nil, nil).VerifyConnection(tls.ConnectionState{}); err == nil {
		t.Fatal("missing certificate accepted")
	}
}

type fixedBotStore struct{ service *botService }

func (s fixedBotStore) Service(context.Context, string, string) (*botService, error) {
	return s.service, nil
}

func TestBotCallbackSurvivesAttemptCancellationAndPreservesRequest(t *testing.T) {
	certificate, leaf := syntheticBotCertificate(t, false)
	var requests atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != "POST" || r.URL.Path != "/base/bots/"+jobUUID+"/messages" || r.Header.Get("Authorization") != "Bearer synthetic-token" || r.Header.Get("Content-Type") != "application/json" {
			t.Error("native callback request changed")
		}
		w.WriteHeader(204)
	}))
	server.TLS = &tls.Config{Certificates: []tls.Certificate{certificate}}
	server.StartTLS()
	defer server.Close()
	service := &botService{URL: server.URL + "/base", Token: "synthetic-token", Fingerprints: [][]byte{botRSAFingerprint(leaf.PublicKey.(*rsa.PublicKey))}, Enabled: true}
	delivery := newExternalBotDelivery(t.Context(), fixedBotStore{service})
	attempt, cancel := context.WithCancel(t.Context())
	cancel()
	delivery.DeliverAsync(attempt, []localConversationMember{{ID: jobUUID, Provider: ptr(jobUUID), Service: ptr(jobUUID)}}, map[string]any{"type": "conversation.member-join"})
	delivery.wg.Wait()
	if requests.Load() != 1 {
		t.Fatal("attempt cancellation suppressed independent callback")
	}
}

func TestBotCallbackRetriesOnlyServerStatusAndSkipsDisabledServices(t *testing.T) {
	certificate, leaf := syntheticBotCertificate(t, false)
	var requests atomic.Int32
	var status atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(int(status.Load()))
	}))
	server.TLS = &tls.Config{Certificates: []tls.Certificate{certificate}}
	server.StartTLS()
	defer server.Close()
	service := &botService{URL: server.URL, Token: "synthetic-token", Fingerprints: [][]byte{botRSAFingerprint(leaf.PublicKey.(*rsa.PublicKey))}, Enabled: true}
	delivery := newExternalBotDelivery(t.Context(), fixedBotStore{service})
	bot := localConversationMember{ID: jobUUID, Provider: ptr(jobUUID), Service: ptr(jobUUID)}
	for _, test := range []struct{ status, count int32 }{{410, 1}, {302, 1}, {503, 4}} {
		status.Store(test.status)
		requests.Store(0)
		if err := delivery.deliver(t.Context(), bot, []byte(`{}`)); err == nil || requests.Load() != test.count {
			t.Fatalf("status %d: count=%d err=%v", test.status, requests.Load(), err)
		}
	}
	requests.Store(0)
	service.Enabled = false
	if err := delivery.deliver(t.Context(), bot, []byte(`{}`)); err != nil || requests.Load() != 0 {
		t.Fatal("disabled bot received a callback")
	}
	delivery.Store = fixedBotStore{}
	if err := delivery.deliver(t.Context(), bot, []byte(`{}`)); err != nil || requests.Load() != 0 {
		t.Fatal("missing bot received a callback")
	}
}
