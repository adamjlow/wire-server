// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/beevik/etree"
	"github.com/decred/dcrd/crypto/ripemd160"
	dsig "github.com/russellhaering/goxmldsig"
)

func signedTestResponse(t *testing.T, bits int) (*etree.Document, *x509.Certificate) {
	return signedTestResponseXML(t, bits, `<samlp:Response xmlns:samlp="`+protocolNS+`" xmlns:saml="`+assertionNS+`" ID="_response"><saml:Issuer>https://synthetic.example/idp</saml:Issuer><saml:Assertion ID="_assertion" Version="2.0"><saml:Issuer>https://synthetic.example/idp</saml:Issuer><saml:Subject><saml:NameID>synthetic-user</saml:NameID></saml:Subject> </saml:Assertion></samlp:Response>`)
}

func signedTestResponseXML(t *testing.T, bits int, source string) (*etree.Document, *x509.Certificate) {
	t.Helper()
	doc, cert, _ := signedTestResponseWithKey(t, bits, source)
	return doc, cert
}

func signedTestResponseWithKey(t *testing.T, bits int, source string) (*etree.Document, *x509.Certificate, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC), NotAfter: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), SignatureAlgorithm: x509.SHA256WithRSA}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	doc := etree.NewDocument()
	if err = doc.ReadFromString(source); err != nil {
		t.Fatal(err)
	}
	ctx, err := dsig.NewSigningContext(key, [][]byte{der})
	if err != nil {
		t.Fatal(err)
	}
	ctx.IdAttribute = "ID"
	ctx.Prefix = "ds"
	ctx.Canonicalizer = dsig.MakeC14N10ExclusiveCanonicalizerWithPrefixList("")
	assertion := etreeChildren(doc.Root(), assertionNS, "Assertion")[0]
	assertion.CreateAttr("xmlns:saml", assertionNS)
	assertion.CreateAttr("xmlns:samlp", protocolNS)
	signed, err := ctx.SignEnveloped(assertion)
	if err != nil {
		t.Fatal(err)
	}
	doc.Root().RemoveChild(assertion)
	doc.Root().AddChild(signed)
	// SDK SignEnveloped appends its signature directly to Child, without fixing
	// etree parent/index bookkeeping. A wire round trip gives fixture mutations
	// the same parsed tree that both actual services receive.
	wire := documentBytes(t, doc)
	normalized := etree.NewDocument()
	if err = normalized.ReadFromBytes(wire); err != nil {
		t.Fatal(err)
	}
	return normalized, cert, key
}

func documentBytes(t *testing.T, doc *etree.Document) []byte {
	t.Helper()
	data, err := doc.WriteToBytes()
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestConfiguredKeyExpiredCertAndVerifiedSubtree(t *testing.T) {
	doc, cert := signedTestResponse(t, 1024)
	nodes, err := verifiedAssertions(documentBytes(t, doc), []*x509.Certificate{cert})
	if err != nil || len(nodes) != 1 || nodes[0].attr("ID") != "_assertion" || len(nodes[0].children(signatureNS, "Signature")) != 0 {
		t.Fatal("valid expired pinned key or verified subtree failed", err)
	}
	// KeyInfo is not authenticated credential selection. Replacing advertised
	// bytes cannot invalidate a valid pinned-key signature or introduce trust.
	sig := etreeChildren(etreeChildren(doc.Root(), assertionNS, "Assertion")[0], signatureNS, "Signature")[0]
	for _, info := range etreeChildren(sig, signatureNS, "KeyInfo") {
		sig.RemoveChild(info)
	}
	bad := sig.CreateElement("ds:KeyInfo")
	bad.CreateElement("ds:X509Data").CreateElement("ds:X509Certificate").SetText("untrusted-advertising")
	if _, err = verifiedAssertions(documentBytes(t, doc), []*x509.Certificate{cert}); err != nil {
		t.Fatal("response KeyInfo was used as trust", err)
	}
	if _, err = verifiedAssertions(documentBytes(t, doc), []*x509.Certificate{cert, cert}); err == nil {
		t.Fatal("duplicate successful credentials accepted")
	}
	_, other := signedTestResponse(t, 1024)
	if _, err = verifiedAssertions(documentBytes(t, doc), []*x509.Certificate{other}); err == nil {
		t.Fatal("unconfigured signing key accepted")
	}
}

func TestSignatureTamperWrappingAndReferenceGuards(t *testing.T) {
	doc, cert := signedTestResponse(t, 1024)
	for _, mutate := range []func(*etree.Element){
		func(root *etree.Element) {
			etreeChildren(root, assertionNS, "Assertion")[0].CreateElement("saml:Subject").SetText("attacker")
		},
		func(root *etree.Element) { root.AddChild(etreeChildren(root, assertionNS, "Assertion")[0].Copy()) },
		func(root *etree.Element) {
			a := etreeChildren(root, assertionNS, "Assertion")[0]
			for _, s := range etreeChildren(a, signatureNS, "Signature") {
				a.RemoveChild(s)
			}
		},
		func(root *etree.Element) {
			a := etreeChildren(root, assertionNS, "Assertion")[0]
			s := etreeChildren(a, signatureNS, "Signature")[0]
			etreeChildren(etreeChildren(s, signatureNS, "SignedInfo")[0], signatureNS, "Reference")[0].SelectAttr("URI").Value = "https://synthetic.example/external"
		},
		func(root *etree.Element) {
			a := etreeChildren(root, assertionNS, "Assertion")[0]
			s := etreeChildren(a, signatureNS, "Signature")[0]
			etreeChildren(etreeChildren(s, signatureNS, "SignedInfo")[0], signatureNS, "Reference")[0].SelectAttr("URI").Value = ""
		},
		func(root *etree.Element) {
			a := etreeChildren(root, assertionNS, "Assertion")[0]
			root.RemoveChild(a)
			root.CreateElement("saml:Advice").AddChild(a)
		},
	} {
		changed := doc.Copy()
		mutate(changed.Root())
		if _, err := verifiedAssertions(documentBytes(t, changed), []*x509.Certificate{cert}); err == nil {
			t.Fatal("tampered/wrapped/unsigned assertion accepted")
		}
	}
}

func TestOriginalMicrosoftSignatureFixture(t *testing.T) {
	data, err := os.ReadFile("../../../libs/saml2-web-sso/test/samples/microsoft-authnresponse-2.xml")
	if err != nil {
		t.Fatal(err)
	}
	n, err := parseXMLNode(data)
	if err != nil {
		t.Fatal(err)
	}
	keys := n.descendants(signatureNS, "KeyInfo")
	if len(keys) != 1 {
		t.Fatal("original certificate fixture changed")
	}
	cert, err := certificateNode(keys[0])
	if err != nil {
		t.Fatal(err)
	}
	verified, err := verifiedAssertions(data, []*x509.Certificate{cert})
	if err != nil {
		t.Fatal("original Microsoft signature rejected", err)
	}
	for _, node := range verified {
		assertion, err := parseSAMLAssertion(node)
		if err != nil || assertion.ID == "" || len(assertion.Statements) != 1 || len(assertion.Confirmations) != 1 {
			t.Fatal("original verified assertion did not parse", err)
		}
	}
	adfs := []byte(strings.ReplaceAll(string(data), "><", "> <"))
	if _, err = verifiedAssertions(adfs, []*x509.Certificate{cert}); err != nil {
		t.Fatal("native ADFS workaround rejected", err)
	}
	for _, bad := range [][]byte{append([]byte("garbage"), data...), []byte(`<!DOCTYPE Response [<!ENTITY injected SYSTEM "file:///synthetic">]><Response xmlns="` + protocolNS + `">&injected;</Response>`)} {
		if _, err = verifiedAssertions(bad, []*x509.Certificate{cert}); err == nil {
			t.Fatal("invalid prologue/entity accepted")
		}
	}
}

func TestGoDefaultRejectsNative768BitFixtureKeys(t *testing.T) {
	// Only fixture generation uses the documented test compatibility switch.
	// Go's default differs from the existing backend. Spar's main package
	// carries a service-local compatibility default preserving native policy.
	t.Setenv("GODEBUG", "rsa1024min=0")
	doc, cert := signedTestResponse(t, 768)
	t.Setenv("GODEBUG", "rsa1024min=1")
	if _, err := verifiedAssertions(documentBytes(t, doc), []*x509.Certificate{cert}); err == nil {
		t.Fatal("Go default weak-key guard was bypassed")
	}
	t.Setenv("GODEBUG", "rsa1024min=0")
	if _, err := verifiedAssertions(documentBytes(t, doc), []*x509.Certificate{cert}); err != nil {
		t.Fatal("native RSA compatibility rejected", err)
	}
	doc.Root().AddChild(etreeChildren(doc.Root(), assertionNS, "Assertion")[0].Copy())
	if _, err := verifiedAssertions(documentBytes(t, doc), []*x509.Certificate{cert}); err == nil {
		t.Fatal("legacy key compatibility bypassed wrapping guard")
	}
}

// Re-sign after changing SignedInfo. Negatives exercise reference selection and
// transforms with a valid RSA signature, rather than merely corrupting the
// original signature. The assertion digest uses the maintained canonicalizer.
func resignTestInfo(t *testing.T, signature *etree.Element, key *rsa.PrivateKey, algorithm crypto.Hash) {
	t.Helper()
	info := etreeChildren(signature, signatureNS, "SignedInfo")[0]
	ctx := dsig.MakeC14N10ExclusiveCanonicalizerWithPrefixList("")
	data, err := ctx.Canonicalize(samlIsolate(info))
	if err != nil {
		t.Fatal(err)
	}
	hash := algorithm.New()
	_, _ = hash.Write(data)
	value, err := rsa.SignPKCS1v15(rand.Reader, key, algorithm, hash.Sum(nil))
	if err != nil {
		t.Fatal(err)
	}
	etreeChildren(signature, signatureNS, "SignatureValue")[0].SetText(base64.StdEncoding.EncodeToString(value))
}

func qualificationResponseSource(kind, attributes string) string {
	issuer := "https://synthetic.example/qualification/" + kind
	instant := "2026-10-10T00:00:00Z"
	return `<samlp:Response xmlns:samlp="` + protocolNS + `" xmlns:saml="` + assertionNS + `" ID="_response" Version="2.0" IssueInstant="` + instant + `"><saml:Issuer>` + issuer + `</saml:Issuer><samlp:Status><samlp:StatusCode Value="urn:oasis:names:tc:SAML:2.0:status:Success"/></samlp:Status><saml:Assertion ID="_assertion" Version="2.0" IssueInstant="` + instant + `"><saml:Issuer>` + issuer + `</saml:Issuer><saml:Subject><saml:NameID Format="` + nameID11 + `emailAddress">synthetic@example.test</saml:NameID><saml:SubjectConfirmation Method="` + bearerMethod + `"><saml:SubjectConfirmationData Recipient="https://synthetic.example/sso/finalize-login" NotOnOrAfter="2030-01-01T00:00:00Z" InResponseTo="_qualification-request"/></saml:SubjectConfirmation></saml:Subject><saml:Conditions NotOnOrAfter="2030-01-01T00:00:00Z"><saml:AudienceRestriction><saml:Audience>https://synthetic.example/sso/finalize-login</saml:Audience></saml:AudienceRestriction></saml:Conditions><saml:AuthnStatement AuthnInstant="` + instant + `"><saml:AuthnContext><saml:AuthnContextClassRef>urn:oasis:names:tc:SAML:2.0:ac:classes:PasswordProtectedTransport</saml:AuthnContextClassRef></saml:AuthnContext></saml:AuthnStatement>` + attributes + `</saml:Assertion></samlp:Response>`
}

// The same finite crypto tests can seed/emit synthetic wire inputs for the
// owned real-native comparison. This optional fixture mode cannot target a
// non-disposable process, run as root, or use any non-test keyspace.
func emitQualificationSignature(t *testing.T, kind string, doc *etree.Document, cert *x509.Certificate, accepted bool) {
	t.Helper()
	configPath := os.Getenv("SPAR_SIGNATURE_FIXTURE_CONFIG")
	if configPath == "" {
		return
	}
	if os.Getuid() != 65534 || os.Getenv("WIRE_E2E_DISPOSABLE") != "1" {
		t.Fatal("fixture mode requires owned nonroot disposable environment")
	}
	config, err := LoadConfig(configPath)
	if err != nil || config.Cassandra.Keyspace != "spar_test" {
		t.Fatal("synthetic signature fixture configuration")
	}
	session, err := openStore(t.Context(), config)
	if err != nil {
		t.Fatal("signature fixture Cassandra startup")
	}
	defer session.Close()
	issuer := "https://synthetic.example/qualification/" + kind
	info := idpConfig{ID: gocql.TimeUUID(), Metadata: idpMetadata{Issuer: issuer, RequestURI: "https://synthetic.example/idp", Certificates: []*x509.Certificate{cert}}, Extra: idpExtra{Team: gocql.TimeUUID(), OldIssuers: []string{}}}
	if err = (cassandraStore{session: session}).insertIdP(t.Context(), info); err != nil {
		t.Fatal("signature fixture seed failed")
	}
	payload, err := json.Marshal(map[string]any{"name": kind, "xml": string(documentBytes(t, doc)), "signature_accepted": accepted, "idp": info.ID.String(), "team": info.Extra.Team.String()})
	if err != nil {
		t.Fatal(err)
	}
	dir := "/reports/signature-fixtures"
	if err = os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, kind+".json"), payload, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestNativeDigestAndLargeAssertionCompatibility(t *testing.T) {
	for _, algorithm := range []string{"sha1", "sha256", "sha512", "ripemd160", "large"} {
		t.Run(algorithm, func(t *testing.T) {
			attributes := ""
			if algorithm == "large" {
				attributes = `<saml:AttributeStatement>` + strings.Repeat(`<saml:Attribute Name="synthetic"><saml:AttributeValue>synthetic</saml:AttributeValue></saml:Attribute>`, 600) + `</saml:AttributeStatement>`
			}
			source := qualificationResponseSource(algorithm, attributes)
			doc, cert, key := signedTestResponseWithKey(t, 1024, source)
			assertion := etreeChildren(doc.Root(), assertionNS, "Assertion")[0]
			sig := etreeChildren(assertion, signatureNS, "Signature")[0]
			info := etreeChildren(sig, signatureNS, "SignedInfo")[0]
			ref := etreeChildren(info, signatureNS, "Reference")[0]
			bare := assertion.Copy()
			bare.RemoveChild(etreeChildren(bare, signatureNS, "Signature")[0])
			canonical, err := dsig.MakeC14N10ExclusiveCanonicalizerWithPrefixList("").Canonicalize(bare)
			if err != nil {
				t.Fatal(err)
			}
			var digest []byte
			uri := "http://www.w3.org/2001/04/xmlenc#" + algorithm
			switch algorithm {
			case "sha1":
				value := sha1.Sum(canonical)
				digest = value[:]
				uri = "http://www.w3.org/2000/09/xmldsig#sha1"
			case "sha256", "large":
				value := sha256.Sum256(canonical)
				digest = value[:]
				uri = "http://www.w3.org/2001/04/xmlenc#sha256"
			case "sha512":
				value := sha512.Sum512(canonical)
				digest = value[:]
			case "ripemd160":
				hash := ripemd160.New()
				_, _ = hash.Write(canonical)
				digest = hash.Sum(nil)
			}
			etreeChildren(ref, signatureNS, "DigestMethod")[0].SelectAttr("Algorithm").Value = uri
			etreeChildren(ref, signatureNS, "DigestValue")[0].SetText(base64.StdEncoding.EncodeToString(digest))
			resignTestInfo(t, sig, key, crypto.SHA256)
			nodes, err := verifiedAssertions(documentBytes(t, doc), []*x509.Certificate{cert})
			if err != nil || len(nodes) != 1 || nodes[0].attr("ID") != "_assertion" {
				t.Fatal("native digest or size rejected", algorithm, err)
			}
			emitQualificationSignature(t, algorithm, doc, cert, true)
			changed := []byte(strings.ReplaceAll(string(documentBytes(t, doc)), "synthetic@example.test", "attacker@example.test"))
			if _, err = verifiedAssertions(changed, []*x509.Certificate{cert}); err == nil {
				t.Fatal("digest trust check bypassed", algorithm)
			}
		})
	}
}

func TestNativeSuccessfulReferenceSelection(t *testing.T) {
	for _, kind := range []string{"missing-extra", "bad-extra-digest", "bad-first-matching", "duplicate-valid", "no-matching", "unsupported-transform", "rsa-sha1"} {
		t.Run(kind, func(t *testing.T) {
			source := qualificationResponseSource(kind, "")
			doc, cert, key := signedTestResponseWithKey(t, 1024, source)
			assertion := etreeChildren(doc.Root(), assertionNS, "Assertion")[0]
			sig := etreeChildren(assertion, signatureNS, "Signature")[0]
			info := etreeChildren(sig, signatureNS, "SignedInfo")[0]
			ref := etreeChildren(info, signatureNS, "Reference")[0]
			accepted := true
			signatureHash := crypto.SHA256
			switch kind {
			case "missing-extra", "bad-extra-digest", "bad-first-matching", "duplicate-valid":
				extra := samlIsolate(ref)
				switch kind {
				case "missing-extra":
					extra.SelectAttr("URI").Value = "#missing"
				case "duplicate-valid":
					accepted = false
				}
				if kind != "duplicate-valid" {
					etreeChildren(extra, signatureNS, "DigestValue")[0].SetText(base64.StdEncoding.EncodeToString(make([]byte, 32)))
				}
				if kind == "bad-first-matching" {
					info.InsertChildAt(ref.Index(), extra)
				} else {
					info.AddChild(extra)
				}
			case "no-matching":
				ref.SelectAttr("URI").Value = "#missing"
				accepted = false
			case "unsupported-transform":
				transforms := etreeChildren(ref, signatureNS, "Transforms")[0]
				transforms.ChildElements()[0].SelectAttr("Algorithm").Value = "urn:synthetic:unsupported"
				accepted = false
			case "rsa-sha1":
				etreeChildren(info, signatureNS, "SignatureMethod")[0].SelectAttr("Algorithm").Value = "http://www.w3.org/2000/09/xmldsig#rsa-sha1"
				signatureHash = crypto.SHA1
			}
			resignTestInfo(t, sig, key, signatureHash)
			_, err := verifiedAssertions(documentBytes(t, doc), []*x509.Certificate{cert})
			if (err == nil) != accepted {
				t.Fatal("native successful matching-reference rule", kind, err)
			}
			emitQualificationSignature(t, kind, doc, cert, accepted)
		})
	}
}
