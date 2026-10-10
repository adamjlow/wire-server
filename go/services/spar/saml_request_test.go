// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"context"
	"encoding/base64"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type requestMemoryStore struct {
	info            idpConfig
	events          []string
	request, issuer string
	format          verdictFormat
}

func (s *requestMemoryStore) getIdP(context.Context, gocql.UUID) (*idpConfig, error) {
	s.events = append(s.events, "idp")
	return &s.info, nil
}
func (s *requestMemoryStore) storeAuthRequest(_ context.Context, id, issuer string, now, end time.Time, maximum TTL) error {
	s.events = append(s.events, "request")
	s.request, s.issuer = id, issuer
	_, err := checkedTTL("authreq", maximum, now, end)
	return err
}
func (s *requestMemoryStore) storeVerdict(_ context.Context, id string, _ float64, format verdictFormat) error {
	s.events = append(s.events, "verdict")
	if id != s.request {
		panic("request correlation changed")
	}
	s.format = format
	return nil
}

func TestSAMLRequestPrecheckAndPersistentIssuerBinding(t *testing.T) {
	c, err := LoadConfig("../../../services/spar/spar.integration.yaml")
	if err != nil {
		t.Fatal(err)
	}
	id, _ := gocql.ParseUUID("00000000-0000-4000-8000-000000000001")
	team, _ := gocql.ParseUUID("00000000-0000-4000-8000-000000000002")
	version, host := "v2", "synthetic.example"
	c.SAML.Domains = map[string]DomainConfig{host: c.SAML.DomainConfig}
	c.SAML.DomainConfig = DomainConfig{}
	store := &requestMemoryStore{info: idpConfig{ID: id, Metadata: idpMetadata{Issuer: "https://synthetic.example/idp", RequestURI: `https://synthetic.example/login?x=<bad>&quoted="value"`}, Extra: idpExtra{Team: team, Version: &version, Domain: &host}}}
	uri := "/sso/initiate-login/" + id.String()
	for _, test := range []struct {
		method, host, query string
		code                int
		events              []string
	}{
		{"HEAD", host, "", 200, []string{"idp"}},
		{"GET", "wrong.example", "", 404, []string{"idp"}},
		{"GET", host, "?success_redirect=wire%3A%2F%2Fsuccess", 400, nil},
		{"GET", host, "?success_redirect=https%3A%2F%2Fsuccess&error_redirect=wire%3A%2F%2Ferror", 400, nil},
		{"GET", host, "?label=synthetic-device", 200, []string{"idp", "idp", "request", "verdict"}},
	} {
		store.events = nil
		r := httptest.NewRequest(test.method, uri+test.query, nil)
		r.Header.Set("Z-Host", test.host)
		w := httptest.NewRecorder()
		if !serveSAMLRequest(w, r, c, store) || w.Code != test.code || !reflect.DeepEqual(store.events, test.events) {
			t.Fatal(test, w.Code, store.events, w.Body.String())
		}
		if test.code == 200 && test.method == "GET" {
			doc, err := parseXMLNode(w.Body.Bytes())
			if err != nil {
				t.Fatal(err)
			}
			inputs := doc.descendants("http://www.w3.org/1999/xhtml", "input")
			if len(inputs) != 2 || inputs[0].attr("name") != "SAMLRequest" {
				t.Fatal("POST binding form changed")
			}
			xml, err := base64.StdEncoding.DecodeString(inputs[0].attr("value"))
			if err != nil {
				t.Fatal(err)
			}
			request, err := parseXMLNode(xml)
			if err != nil {
				t.Fatal(err)
			}
			issuer := request.children(assertionNS, "Issuer")
			if request.Name.Space != protocolNS || request.attr("ID") != store.request || store.issuer != store.info.Metadata.Issuer || len(issuer) != 1 || issuer[0].Text != "http://localhost:8088/sso/finalize-login/"+team.String() {
				t.Fatal("SP/request-to-IdP binding changed")
			}
			if store.format.Label == nil || *store.format.Label != "synthetic-device" {
				t.Fatal("device label not persisted")
			}
		}
	}
}

func TestSPMetadataDomainAndContactEncoding(t *testing.T) {
	c, err := LoadConfig("../../../services/spar/spar.integration.yaml")
	if err != nil {
		t.Fatal(err)
	}
	c.SAML.SSOURI = "HTTPS://SYNTHETIC.EXAMPLE/a//b/../sso?z=2&a=1"
	uri, err := responseURI(c, "ignored.single-ingress", nil)
	if err != nil || uri != "https://synthetic.example/a/sso/finalize-login?a=1&z=2" {
		t.Fatal(uri, err)
	}
	contact := map[string]any{"type": "ContactSupport", "givenName": "synthetic <contact>", "email": "mailto:synthetic@example.test"}
	doc, err := parseXMLNode([]byte(metadataXML("_synthetic", uri, time.Now(), []map[string]any{contact})))
	if err != nil || doc.Name.Space != metadataNS || doc.attr("entityID") != uri {
		t.Fatal("SP metadata encoding", err)
	}
	acs := doc.descendants(metadataNS, "AssertionConsumerService")
	names := doc.descendants(metadataNS, "GivenName")
	if len(acs) != 1 || acs[0].attr("Location") != uri || len(names) != 1 || names[0].Text != contact["givenName"] {
		t.Fatal("SP metadata contents or escaping changed")
	}
	c.SAML.Domains = map[string]DomainConfig{"synthetic.example": c.SAML.DomainConfig}
	if _, err = responseURI(c, "missing.example", nil); err == nil {
		t.Fatal("unknown ingress served SP metadata")
	}
	for _, bad := range []map[string]any{{"type": "Unsupported"}, {"type": "ContactSupport", "email": "not-an-uri"}, {"type": "ContactSupport", "givenName": 7}} {
		d := DomainConfig{AppURI: "https://synthetic.example", SSOURI: "https://synthetic.example/sso", Contacts: []map[string]any{bad}}
		if validDomain(d) {
			t.Fatal("invalid native contact accepted")
		}
	}
}

func TestMobileRedirectQueryBounds(t *testing.T) {
	for _, test := range []struct {
		success, error string
		allowed        bool
	}{
		{"wire://success/$userid", "wire://error/$label", true},
		{"wirecustom://success", "wire://error", true},
		{"https://success", "wire://error", false},
		{"WIRE://success", "wire://error", false},
		{"wire://success/" + strings.Repeat("x", 140), "wire://error", false},
	} {
		q := url.Values{"success_redirect": {test.success}, "error_redirect": {test.error}}
		format, err := authRequestFormat(q)
		if (err == nil) != test.allowed || (err == nil && !format.Mobile) {
			t.Fatal(test, err)
		}
	}
}
