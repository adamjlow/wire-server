// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type finalizeBoundary struct {
	*loginBoundary
	*judgeMemory
	idp    idpConfig
	format verdictFormat
}

func (b *finalizeBoundary) idpByIssuer(_ context.Context, issuer string, version int, _ gocql.UUID) (*idpConfig, error) {
	if issuer != b.idp.Metadata.Issuer || version != 1 {
		return nil, errIdPNotFound
	}
	return &b.idp, nil
}
func (b *finalizeBoundary) getVerdict(context.Context, string) (*verdictFormat, error) {
	return &b.format, nil
}

func TestFinalizeVerifiedSessionAndSecurityDenials(t *testing.T) {
	for _, kind := range []string{"valid", "wrong-audience", "replay", "unsolicited", "tampered"} {
		t.Run(kind, func(t *testing.T) {
			now := time.Now().UTC()
			issued := now.Add(-time.Minute).Format(time.RFC3339Nano)
			end := now.Add(5 * time.Minute).Format(time.RFC3339Nano)
			issuer := "https://synthetic.example/idp"
			recipient := "https://synthetic.example/sso/finalize-login"
			audience := recipient
			request := ` InResponseTo="_request"`
			if kind == "wrong-audience" {
				audience = "https://other.example/sp"
			}
			if kind == "unsolicited" {
				request = ""
			}
			source := `<samlp:Response xmlns:samlp="` + protocolNS + `" xmlns:saml="` + assertionNS + `" ID="_response" Version="2.0" IssueInstant="` + issued + `"><saml:Issuer>` + issuer + `</saml:Issuer><samlp:Status><samlp:StatusCode Value="urn:oasis:names:tc:SAML:2.0:status:Success"/></samlp:Status><saml:Assertion ID="_assertion" Version="2.0" IssueInstant="` + issued + `"><saml:Issuer>` + issuer + `</saml:Issuer><saml:Subject><saml:NameID Format="` + nameID11 + `emailAddress">synthetic@example.test</saml:NameID><saml:SubjectConfirmation Method="` + bearerMethod + `"><saml:SubjectConfirmationData Recipient="` + recipient + `" NotOnOrAfter="` + end + `"` + request + `/></saml:SubjectConfirmation></saml:Subject><saml:Conditions NotOnOrAfter="` + end + `"><saml:AudienceRestriction><saml:Audience>` + audience + `</saml:Audience></saml:AudienceRestriction></saml:Conditions><saml:AuthnStatement AuthnInstant="` + issued + `"><saml:AuthnContext><saml:AuthnContextClassRef>urn:oasis:names:tc:SAML:2.0:ac:classes:PasswordProtectedTransport</saml:AuthnContextClassRef></saml:AuthnContext></saml:AuthnStatement></saml:Assertion></samlp:Response>`
			doc, cert := signedTestResponseXML(t, 1024, source)
			data := documentBytes(t, doc)
			if kind == "tampered" {
				data = []byte(strings.ReplaceAll(string(data), "synthetic@example.test", "attacker@example.test"))
			}
			b, idp, ref, user := loginFixture(t)
			ref.Issuer = issuer
			b.mappings[issuer] = user
			b.accounts[user] = &brigAccount{ID: user, Team: &b.team}
			idp.Metadata = idpMetadata{Issuer: issuer, Certificates: []*x509.Certificate{cert}}
			boundary := &finalizeBoundary{loginBoundary: b, judgeMemory: &judgeMemory{issuer: &issuer, alive: kind == "replay"}, idp: idp}
			ttl := TTL(3600)
			config := Config{SAML: SAMLConfig{DomainConfig: DomainConfig{SSOURI: "https://synthetic.example/sso"}}, MaxAssertionTTL: &ttl}
			form := url.Values{"SAMLResponse": {base64.StdEncoding.EncodeToString(data)}}
			r := httptest.NewRequest("POST", "/sso/finalize-login", strings.NewReader(form.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()
			if !serveSAMLFinalize(w, r, config, boundary, b) {
				t.Fatal("finalization not dispatched")
			}
			if kind == "valid" {
				if w.Code != 200 || w.Header().Get("Set-Cookie") != "zuid=synthetic" || !strings.Contains(w.Body.String(), "wire:sso:success") || len(b.events) != 3 || b.events[2] != "session" {
					t.Fatal("verified login failed", w.Code, b.events, w.Body.String())
				}
			} else {
				if w.Header().Get("Set-Cookie") != "" || len(b.events) != 0 {
					t.Fatal("invalid login reached identity/session effects", kind, b.events)
				}
				if kind == "unsolicited" {
					if w.Code != 303 || !strings.HasPrefix(w.Header().Get("Location"), "/sso/initiate-login/") {
						t.Fatal("unsolicited grant/redirect mismatch")
					}
				} else if kind == "tampered" {
					if w.Code != 400 || !strings.Contains(w.Body.String(), "bad-response-signature") {
						t.Fatal("tamper accepted")
					}
				} else if w.Code != 200 || !strings.Contains(w.Body.String(), "wire:sso:error:forbidden") {
					t.Fatal("denial verdict mismatch", w.Code, w.Body.String())
				}
			}
		})
	}
}
