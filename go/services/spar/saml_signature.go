// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"bytes"
	"crypto"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"hash"
	"sort"
	"strings"

	"github.com/beevik/etree"
	"github.com/decred/dcrd/crypto/ripemd160"
	dsig "github.com/russellhaering/goxmldsig"
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
	// SDK's optional ValidateInput rejects even a trailing newline. Build the
	// namespace tree after complete single-root standard-parser validation.
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
		isolated := samlIsolate(assertion)
		accepted, err := verifyPinnedAssertion(isolated, certificates)
		if err != nil {
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

// Copy inherited namespace declarations onto the selected subtree. This uses
// etree's namespace representation, without the SDK traversal helper's unrelated
// 1000-element cap. Canonicalization still comes from the maintained library.
func samlIsolate(element *etree.Element) *etree.Element {
	declarations := map[string]string{}
	var ancestors []*etree.Element
	for parent := element.Parent(); parent != nil; parent = parent.Parent() {
		ancestors = append(ancestors, parent)
	}
	for i := len(ancestors) - 1; i >= 0; i-- {
		for _, attr := range ancestors[i].Attr {
			if attr.Space == "xmlns" {
				declarations["xmlns:"+attr.Key] = attr.Value
			}
			if attr.Space == "" && attr.Key == "xmlns" {
				declarations["xmlns"] = attr.Value
			}
		}
	}
	copy := element.Copy()
	keys := make([]string, 0, len(declarations))
	for key := range declarations {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if copy.SelectAttr(key) == nil {
			copy.CreateAttr(key, declarations[key])
		}
	}
	return copy
}

func samlCanonicalizer(method *etree.Element) (dsig.Canonicalizer, error) {
	prefix := ""
	children := method.ChildElements()
	if len(children) > 1 {
		return nil, errSAMLSignature
	}
	if len(children) == 1 {
		child := children[0]
		if child.Tag != "InclusiveNamespaces" || child.NamespaceURI() != "http://www.w3.org/2001/10/xml-exc-c14n#" {
			return nil, errSAMLSignature
		}
		prefix = child.SelectAttrValue("PrefixList", "")
	}
	switch method.SelectAttrValue("Algorithm", "") {
	case "http://www.w3.org/2001/10/xml-exc-c14n#":
		return dsig.MakeC14N10ExclusiveCanonicalizerWithPrefixList(prefix), nil
	case "http://www.w3.org/2001/10/xml-exc-c14n#WithComments":
		return dsig.MakeC14N10ExclusiveWithCommentsCanonicalizerWithPrefixList(prefix), nil
	case "http://www.w3.org/TR/2001/REC-xml-c14n-20010315":
		if prefix != "" {
			return nil, errSAMLSignature
		}
		return dsig.MakeC14N10RecCanonicalizer(), nil
	case "http://www.w3.org/TR/2001/REC-xml-c14n-20010315#WithComments":
		if prefix != "" {
			return nil, errSAMLSignature
		}
		return dsig.MakeC14N10WithCommentsCanonicalizer(), nil
	case "http://www.w3.org/2006/12/xml-c14n11":
		if prefix != "" {
			return nil, errSAMLSignature
		}
		return dsig.MakeC14N11Canonicalizer(), nil
	case "http://www.w3.org/2006/12/xml-c14n11#WithComments":
		if prefix != "" {
			return nil, errSAMLSignature
		}
		return dsig.MakeC14N11WithCommentsCanonicalizer(), nil
	}
	return nil, errSAMLSignature
}

func signatureDigest(method *etree.Element) (hash.Hash, error) {
	if len(method.ChildElements()) != 0 {
		return nil, errSAMLSignature
	}
	switch method.SelectAttrValue("Algorithm", "") {
	case "http://www.w3.org/2000/09/xmldsig#sha1":
		return sha1.New(), nil
	case "http://www.w3.org/2001/04/xmlenc#sha256":
		return sha256.New(), nil
	case "http://www.w3.org/2001/04/xmlenc#sha512":
		return sha512.New(), nil
	case "http://www.w3.org/2001/04/xmlenc#ripemd160":
		return ripemd160.New(), nil
	}
	return nil, errSAMLSignature
}

// Native verifyReference returns a failed result for missing/non-local IDs and
// bad digests; verifySignature selects exactly one successful assertion-ID
// reference. Unsupported transforms/digests on an existing subtree are fatal.
// Never fetch reference URIs or use an unsigned KeyInfo as a credential source.
func samlReference(assertion, reference *etree.Element) (string, bool, error) {
	uri := reference.SelectAttrValue("URI", "")
	if !strings.HasPrefix(uri, "#") || len(uri) == 1 {
		return "", false, nil
	}
	id := uri[1:]
	var selected *etree.Element
	stack := []*etree.Element{assertion}
	for len(stack) > 0 {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if node.SelectAttrValue("ID", "") == id {
			if selected != nil {
				return "", false, nil
			}
			selected = node
		}
		stack = append(stack, node.ChildElements()...)
	}
	if selected == nil {
		return "", false, nil
	}
	transformed := samlIsolate(selected)
	var encoded []byte
	canonicalized := false
	groups := etreeChildren(reference, signatureNS, "Transforms")
	if len(groups) > 1 {
		return "", false, errSAMLSignature
	}
	if len(groups) == 1 {
		for _, transform := range groups[0].ChildElements() {
			if transform.Tag != "Transform" || transform.NamespaceURI() != signatureNS || canonicalized {
				return "", false, errSAMLSignature
			}
			if transform.SelectAttrValue("Algorithm", "") == "http://www.w3.org/2000/09/xmldsig#enveloped-signature" {
				if len(transform.ChildElements()) != 0 {
					return "", false, errSAMLSignature
				}
				for _, sig := range etreeChildren(transformed, signatureNS, "Signature") {
					transformed.RemoveChild(sig)
				}
			} else {
				canonicalizer, err := samlCanonicalizer(transform)
				if err != nil {
					return "", false, err
				}
				encoded, err = canonicalizer.Canonicalize(transformed)
				if err != nil {
					return "", false, err
				}
				canonicalized = true
			}
		}
	}
	if !canonicalized {
		var err error
		encoded, err = dsig.MakeNullCanonicalizer().Canonicalize(transformed)
		if err != nil {
			return "", false, err
		}
	}
	methods := etreeChildren(reference, signatureNS, "DigestMethod")
	values := etreeChildren(reference, signatureNS, "DigestValue")
	if len(methods) != 1 || len(values) != 1 {
		return "", false, errSAMLSignature
	}
	digest, err := signatureDigest(methods[0])
	if err != nil {
		return "", false, err
	}
	expected, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(values[0].Text()), ""))
	if err != nil {
		return "", false, errSAMLSignature
	}
	_, _ = digest.Write(encoded)
	return id, bytes.Equal(expected, digest.Sum(nil)), nil
}

func verifyPinnedAssertion(assertion *etree.Element, certificates []*x509.Certificate) (*etree.Element, error) {
	signatures := etreeChildren(assertion, signatureNS, "Signature")
	if len(signatures) != 1 {
		return nil, errSAMLSignature
	}
	infos := etreeChildren(signatures[0], signatureNS, "SignedInfo")
	values := etreeChildren(signatures[0], signatureNS, "SignatureValue")
	if len(infos) != 1 || len(values) != 1 {
		return nil, errSAMLSignature
	}
	info := infos[0]
	methods := etreeChildren(info, signatureNS, "SignatureMethod")
	canonicalMethods := etreeChildren(info, signatureNS, "CanonicalizationMethod")
	if len(methods) != 1 || len(canonicalMethods) != 1 || len(methods[0].ChildElements()) != 0 {
		return nil, errSAMLSignature
	}
	var signatureHash crypto.Hash
	switch methods[0].SelectAttrValue("Algorithm", "") {
	case "http://www.w3.org/2000/09/xmldsig#rsa-sha1":
		signatureHash = crypto.SHA1
	case "http://www.w3.org/2001/04/xmldsig-more#rsa-sha256":
		signatureHash = crypto.SHA256
	default:
		return nil, errSAMLSignature
	}
	canonicalizer, err := samlCanonicalizer(canonicalMethods[0])
	if err != nil {
		return nil, err
	}
	canonical, err := canonicalizer.Canonicalize(samlIsolate(info))
	if err != nil {
		return nil, err
	}
	matched := 0
	for _, reference := range etreeChildren(info, signatureNS, "Reference") {
		id, valid, err := samlReference(assertion, reference)
		if err != nil {
			return nil, err
		}
		if valid && id == assertion.SelectAttrValue("ID", "") {
			matched++
		}
	}
	if matched != 1 {
		return nil, errSAMLSignature
	}
	value, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(values[0].Text()), ""))
	if err != nil {
		return nil, errSAMLSignature
	}
	hash := signatureHash.New()
	_, _ = hash.Write(canonical)
	successes := 0
	for _, cert := range certificates {
		// Native certToCreds uses the stored RSA/SHA256 certificate as a pinned
		// public key, without web-PKI date validation or a new modulus policy.
		if cert == nil || cert.SignatureAlgorithm != x509.SHA256WithRSA {
			return nil, errSAMLSignature
		}
		key, ok := cert.PublicKey.(*rsa.PublicKey)
		if !ok {
			return nil, errSAMLSignature
		}
		if rsa.VerifyPKCS1v15(key, signatureHash, hash.Sum(nil), value) == nil {
			successes++
		}
	}
	if successes != 1 {
		return nil, errSAMLSignature
	}
	// Return a copy of the uniquely digest-verified assertion; nothing outside
	// this subtree can contribute identity fields to login.
	accepted := assertion.Copy()
	for _, signature := range etreeChildren(accepted, signatureNS, "Signature") {
		accepted.RemoveChild(signature)
	}
	return accepted, nil
}
