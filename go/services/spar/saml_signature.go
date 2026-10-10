// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"crypto/rsa"
	"crypto/x509"
	"errors"
	"strings"
	"time"

	"github.com/beevik/etree"
	dsig "github.com/russellhaering/goxmldsig"
	"github.com/russellhaering/goxmldsig/etreeutils"
)

var errSAMLSignature = errors.New("invalid SAML signature")

func etreeChildren(node *etree.Element, namespace, tag string) []*etree.Element {
	children := []*etree.Element{}
	for _, child := range node.ChildElements() {
		if child.Tag == tag && child.NamespaceURI() == namespace {
			children = append(children, child)
		}
	}
	return children
}

// Verify only top-level assertions and return only the validator's verified
// referenced subtree. Parsing unsigned siblings or reusing the unverified
// original tree for login would make a valid signature a wrapping shortcut.
func verifiedAssertions(encoded []byte, certificates []*x509.Certificate) ([]*xmlNode, error) {
	verified, err := verifyAssertionDocument(encoded, certificates)
	if err == nil {
		return verified, nil
	}
	// Native allVerifies first verifies the untouched document. Only a failed
	// verification triggers the precise ADFS fix, across all assertions again.
	// Always trimming would reject valid signatures over actual whitespace.
	tweaked := strings.ReplaceAll(string(encoded), "> <", "><")
	if tweaked != string(encoded) {
		if repaired, retryErr := verifyAssertionDocument([]byte(tweaked), certificates); retryErr == nil {
			return repaired, nil
		}
	}
	return nil, err
}

func verifyAssertionDocument(encoded []byte, certificates []*x509.Certificate) ([]*xmlNode, error) {
	// Encoding/xml rejects ambiguous expanded attributes and external entities
	// before the namespace-aware signature tree is built.
	if _, err := parseXMLNode(encoded); err != nil {
		return nil, errSAMLSignature
	}
	doc := etree.NewDocument()
	// The strict preflight above admits XML prologue/trailing whitespace. The
	// SDK's optional ValidateInput rejects even a trailing newline; use its
	// normal non-permissive parser after our complete single-root validation.
	if err := doc.ReadFromBytes(encoded); err != nil {
		return nil, errSAMLSignature
	}
	root := doc.Root()
	if root == nil || root.Tag != "Response" || root.NamespaceURI() != protocolNS {
		return nil, errSAMLSignature
	}
	ids := map[string]bool{}
	stack := []*etree.Element{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if attr := n.SelectAttr("ID"); attr != nil {
			if attr.Value == "" || ids[attr.Value] {
				return nil, errSAMLSignature
			}
			ids[attr.Value] = true
		}
		stack = append(stack, n.ChildElements()...)
	}
	assertions := etreeChildren(root, assertionNS, "Assertion")
	if len(assertions) == 0 {
		return nil, errSAMLSignature
	}
	verified := make([]*xmlNode, 0, len(assertions))
	for _, assertion := range assertions {
		id := assertion.SelectAttrValue("ID", "")
		signatures := etreeChildren(assertion, signatureNS, "Signature")
		if id == "" || len(signatures) != 1 {
			return nil, errSAMLSignature
		}
		sig := signatures[0]
		infos := etreeChildren(sig, signatureNS, "SignedInfo")
		if len(infos) != 1 {
			return nil, errSAMLSignature
		}
		methods := etreeChildren(infos[0], signatureNS, "SignatureMethod")
		if len(methods) != 1 {
			return nil, errSAMLSignature
		}
		algorithm := methods[0].SelectAttrValue("Algorithm", "")
		if algorithm != "http://www.w3.org/2000/09/xmldsig#rsa-sha1" && algorithm != "http://www.w3.org/2001/04/xmldsig-more#rsa-sha256" {
			return nil, errSAMLSignature
		}
		matched := 0
		for _, reference := range etreeChildren(infos[0], signatureNS, "Reference") {
			digests := etreeChildren(reference, signatureNS, "DigestMethod")
			if len(digests) != 1 {
				return nil, errSAMLSignature
			}
			switch digests[0].SelectAttrValue("Algorithm", "") {
			case "http://www.w3.org/2000/09/xmldsig#sha1", "http://www.w3.org/2001/04/xmlenc#sha256", "http://www.w3.org/2001/04/xmlenc#sha512", "http://www.w3.org/2001/04/xmlenc#ripemd160":
			default:
				return nil, errSAMLSignature
			}
			uri := reference.SelectAttrValue("URI", "")
			if !strings.HasPrefix(uri, "#") || len(uri) == 1 || !ids[uri[1:]] {
				return nil, errSAMLSignature
			}
			if uri == "#"+id {
				matched++
			}
		}
		if matched != 1 {
			return nil, errSAMLSignature
		}
		parent, err := etreeutils.NSBuildParentContext(assertion)
		if err != nil {
			return nil, errSAMLSignature
		}
		isolated, err := etreeutils.NSDetatch(parent, assertion)
		if err != nil {
			return nil, errSAMLSignature
		}
		// KeyInfo is unsigned credential advertising, not a trust store. Its
		// removal cannot affect an enveloped assertion digest or SignedInfo.
		isolatedSignature := etreeChildren(isolated, signatureNS, "Signature")[0]
		for _, keyInfo := range etreeChildren(isolatedSignature, signatureNS, "KeyInfo") {
			isolatedSignature.RemoveChild(keyInfo)
		}
		var accepted *etree.Element
		successes := 0
		for _, cert := range certificates {
			// This is the native certToCreds boundary, separate from the XML
			// signature's RSA-SHA1/SHA256 algorithm selection.
			if cert == nil || cert.SignatureAlgorithm != x509.SHA256WithRSA {
				return nil, errSAMLSignature
			}
			if _, ok := cert.PublicKey.(*rsa.PublicKey); !ok {
				return nil, errSAMLSignature
			}
			trusted := *cert
			// Native stored certificates are pinned signing keys, not dated
			// web-PKI identities. Override only the SDK copy's date check;
			// SAML assertion time checks must use the real request clock.
			trusted.NotBefore = time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)
			trusted.NotAfter = time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)
			validator := dsig.NewDefaultValidationContext(&dsig.MemoryX509CertificateStore{Roots: []*x509.Certificate{&trusted}})
			validator.IdAttribute = "ID"
			value, err := validator.Validate(isolated)
			if err == nil {
				accepted = value
				successes++
			}
		}
		// Duplicate successful credentials are rejected by native verifyIO.
		if successes != 1 {
			return nil, errSAMLSignature
		}
		out := etree.NewDocument()
		out.SetRoot(accepted)
		serialized, err := out.WriteToBytes()
		if err != nil {
			return nil, errSAMLSignature
		}
		node, err := parseXMLNode(serialized)
		if err != nil || node.Name.Space != assertionNS || node.Name.Local != "Assertion" || node.attr("ID") != id {
			return nil, errSAMLSignature
		}
		verified = append(verified, node)
	}
	return verified, nil
}
