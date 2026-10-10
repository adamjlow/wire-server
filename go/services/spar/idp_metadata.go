// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"io"
	"strings"
)

const metadataNS = "urn:oasis:names:tc:SAML:2.0:metadata"
const signatureNS = "http://www.w3.org/2000/09/xmldsig#"

type xmlNode struct {
	Name       xml.Name
	Attributes []xml.Attr
	Text       string
	Children   []*xmlNode
}

func (n *xmlNode) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	n.Name = start.Name
	n.Attributes = start.Attr
	seen := map[xml.Name]bool{}
	for _, attr := range start.Attr {
		if seen[attr.Name] {
			return errors.New("duplicate XML attribute")
		}
		seen[attr.Name] = true
	}
	for {
		token, err := d.Token()
		if err != nil {
			return err
		}
		switch value := token.(type) {
		case xml.StartElement:
			child := &xmlNode{}
			if err = d.DecodeElement(child, &value); err != nil {
				return err
			}
			n.Children = append(n.Children, child)
		case xml.CharData:
			n.Text += string(value)
		case xml.EndElement:
			return nil
		}
	}
}
func parseXMLNode(encoded []byte) (*xmlNode, error) {
	decoder := xml.NewDecoder(strings.NewReader(string(encoded)))
	var node xmlNode
	for {
		token, err := decoder.Token()
		if err != nil {
			return nil, errors.New("invalid XML document")
		}
		if start, ok := token.(xml.StartElement); ok {
			if decoder.DecodeElement(&node, &start) != nil {
				return nil, errors.New("invalid XML document")
			}
			break
		}
		if text, ok := token.(xml.CharData); ok && strings.TrimSpace(string(text)) != "" {
			return nil, errors.New("invalid XML document")
		}
	}
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return &node, nil
		}
		if err != nil {
			return nil, errors.New("invalid XML document")
		}
		switch value := token.(type) {
		case xml.StartElement:
			return nil, errors.New("multiple XML roots")
		case xml.CharData:
			if strings.TrimSpace(string(value)) != "" {
				return nil, errors.New("invalid XML document")
			}
		}
	}
}
func (n *xmlNode) attr(name string) string {
	for _, attr := range n.Attributes {
		if attr.Name.Space == "" && attr.Name.Local == name {
			return attr.Value
		}
	}
	return ""
}
func (n *xmlNode) children(space, local string) []*xmlNode {
	nodes := []*xmlNode{}
	for _, child := range n.Children {
		if child.Name == (xml.Name{Space: space, Local: local}) {
			nodes = append(nodes, child)
		}
	}
	return nodes
}
func (n *xmlNode) descendants(space, local string) []*xmlNode {
	nodes := []*xmlNode{}
	for _, child := range n.Children {
		if child.Name == (xml.Name{Space: space, Local: local}) {
			nodes = append(nodes, child)
		}
		nodes = append(nodes, child.descendants(space, local)...)
	}
	return nodes
}
func certificateNode(key *xmlNode) (*x509.Certificate, error) {
	bad := errors.New("invalid stored IdP certificate")
	if key.Name != (xml.Name{Space: signatureNS, Local: "KeyInfo"}) {
		return nil, bad
	}
	data := []*xmlNode{}
	for _, child := range key.Children {
		if child.Name == (xml.Name{Space: signatureNS, Local: "KeyName"}) {
			continue
		}
		data = append(data, child)
	}
	if len(data) != 1 || data[0].Name != (xml.Name{Space: signatureNS, Local: "X509Data"}) || len(data[0].Children) != 1 {
		return nil, bad
	}
	cert := data[0].Children[0]
	if cert.Name != (xml.Name{Space: signatureNS, Local: "X509Certificate"}) || len(cert.Children) != 0 {
		return nil, bad
	}
	der, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(cert.Text), ""))
	if err != nil {
		return nil, bad
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, bad
	}
	return parsed, nil
}
func parseIdPMetadata(encoded string) (idpMetadata, error) {
	var result idpMetadata
	root, err := parseXMLNode([]byte(encoded))
	if err != nil {
		return result, err
	}
	if root.Name == (xml.Name{Space: metadataNS, Local: "EntitiesDescriptor"}) {
		if len(root.Children) != 1 {
			return result, errors.New("expected one metadata entity")
		}
		root = root.Children[0]
	}
	if root.Name != (xml.Name{Space: metadataNS, Local: "EntityDescriptor"}) {
		return result, errors.New("expected metadata entity")
	}
	result.Issuer = root.attr("entityID")
	if !absoluteURI(result.Issuer) {
		return result, errors.New("invalid IdP issuer")
	}
	endpoints := []string{}
	for _, descriptor := range root.descendants(metadataNS, "IDPSSODescriptor") {
		for _, service := range descriptor.children(metadataNS, "SingleSignOnService") {
			if strings.EqualFold(service.attr("Binding"), "urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST") {
				endpoints = append(endpoints, service.attr("Location"))
			}
		}
		for _, key := range descriptor.children(metadataNS, "KeyDescriptor") {
			if key.attr("use") == "encryption" {
				continue
			}
			for _, info := range key.children(signatureNS, "KeyInfo") {
				cert, err := certificateNode(info)
				if err != nil {
					return result, err
				}
				result.Certificates = append(result.Certificates, cert)
			}
		}
	}
	if len(endpoints) != 1 || !absoluteURI(endpoints[0]) {
		return result, errors.New("expected one HTTP-POST SSO endpoint")
	}
	result.RequestURI = endpoints[0]
	if len(result.Certificates) == 0 {
		return result, errors.New("missing IdP signing certificate")
	}
	return result, nil
}
