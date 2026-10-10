// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/beevik/etree"
	dsig "github.com/russellhaering/goxmldsig"
)

func signedTestResponse(t *testing.T, bits int) (*etree.Document, *x509.Certificate) {
	return signedTestResponseXML(t, bits, `<samlp:Response xmlns:samlp="`+protocolNS+`" xmlns:saml="`+assertionNS+`" ID="_response"><saml:Issuer>https://synthetic.example/idp</saml:Issuer><saml:Assertion ID="_assertion" Version="2.0"><saml:Issuer>https://synthetic.example/idp</saml:Issuer><saml:Subject><saml:NameID>synthetic-user</saml:NameID></saml:Subject> </saml:Assertion></samlp:Response>`)
}

func signedTestResponseXML(t *testing.T, bits int, source string) (*etree.Document, *x509.Certificate) {
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
	return doc, cert
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
