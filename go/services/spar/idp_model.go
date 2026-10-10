// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"github.com/apache/cassandra-gocql-driver/v2"
	"strconv"
)

type idpMetadata struct {
	Issuer       string              `json:"issuer"`
	RequestURI   string              `json:"requestURI"`
	Certificates []*x509.Certificate `json:"-"`
}

func renderCertificate(cert *x509.Certificate) string {
	return `<ds:KeyInfo xmlns:ds="http://www.w3.org/2000/09/xmldsig#"><ds:X509Data><ds:X509Certificate>` + base64.StdEncoding.EncodeToString(cert.Raw) + `</ds:X509Certificate></ds:X509Data></ds:KeyInfo>`
}
func (m idpMetadata) MarshalJSON() ([]byte, error) {
	keys := make([]string, len(m.Certificates))
	for i, cert := range m.Certificates {
		keys[i] = renderCertificate(cert)
	}
	return json.Marshal(struct {
		Issuer     string   `json:"issuer"`
		RequestURI string   `json:"requestURI"`
		Keys       []string `json:"certAuthnResponse"`
	}{m.Issuer, m.RequestURI, keys})
}
func (m *idpMetadata) UnmarshalJSON(data []byte) error {
	var value struct {
		Issuer     string   `json:"issuer"`
		RequestURI string   `json:"requestURI"`
		Keys       []string `json:"certAuthnResponse"`
	}
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	m.Issuer = value.Issuer
	m.RequestURI = value.RequestURI
	m.Certificates = nil
	for _, key := range value.Keys {
		cert, err := decodeStoredCertificate([]byte(key))
		if err != nil {
			return err
		}
		m.Certificates = append(m.Certificates, cert)
	}
	return nil
}

type idpExtra struct {
	Team       gocql.UUID  `json:"team"`
	Version    *string     `json:"apiVersion"`
	OldIssuers []string    `json:"oldIssuers"`
	ReplacedBy *gocql.UUID `json:"replacedBy"`
	Handle     string      `json:"handle"`
	Domain     *string     `json:"domain"`
}
type idpConfig struct {
	ID       gocql.UUID  `json:"id"`
	Metadata idpMetadata `json:"metadata"`
	Extra    idpExtra    `json:"extraInfo"`
}

// Query/storage versions are v1/v2, but the native JSON enum has constructor
// names. Brig decodes this representation for multi-ingress notifications.
func (e idpExtra) MarshalJSON() ([]byte, error) {
	type fields idpExtra
	out := fields(e)
	if e.Version != nil {
		version := "WireIdPAPIV1"
		if *e.Version == "v2" {
			version = "WireIdPAPIV2"
		}
		out.Version = &version
	}
	return json.Marshal(out)
}

func (e *idpExtra) UnmarshalJSON(data []byte) error {
	type fields idpExtra
	var value fields
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	if value.Version != nil {
		version := "v1"
		switch *value.Version {
		case "WireIdPAPIV1":
		case "WireIdPAPIV2":
			version = "v2"
		default:
			return &apiError{400, "bad-request", "Invalid IdP API version"}
		}
		value.Version = &version
	}
	*e = idpExtra(value)
	return nil
}

func (i idpConfig) version() int {
	if i.Extra.Version != nil && *i.Extra.Version == "v2" {
		return 2
	}
	return 1
}
func uniqueIdPHandle(handles []string) string {
	for n := 1; ; n++ {
		name := "IdP " + strconv.Itoa(n)
		found := false
		for _, handle := range handles {
			if name == handle {
				found = true
				break
			}
		}
		if !found {
			return name
		}
	}
}
