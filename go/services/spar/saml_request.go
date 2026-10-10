// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

const protocolNS = "urn:oasis:names:tc:SAML:2.0:protocol"
const postBinding = "urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST"

type samlRequestStore interface {
	getIdP(context.Context, gocql.UUID) (*idpConfig, error)
	storeAuthRequest(context.Context, string, string, time.Time, time.Time, TTL) error
	storeVerdict(context.Context, string, float64, verdictFormat) error
}

func domainConfig(c Config, host string) (DomainConfig, bool) {
	if c.SAML.Domains == nil {
		return c.SAML.DomainConfig, true
	}
	d, ok := c.SAML.Domains[host]
	return d, ok
}

func responseURI(c Config, host string, team *gocql.UUID) (string, error) {
	d, ok := domainConfig(c, host)
	if !ok {
		return "", &apiError{404, "not-found", "Could not find SP."}
	}
	u, err := url.Parse(d.SSOURI)
	if err != nil {
		return "", errors.New("invalid configured SP URI")
	}
	u.Scheme, u.Host = strings.ToLower(u.Scheme), strings.ToLower(u.Host)
	suffix := "finalize-login"
	if team != nil {
		suffix += "/" + team.String()
	}
	u.RawPath = path.Clean(u.EscapedPath() + "/" + suffix)
	u.Path, err = url.PathUnescape(u.RawPath)
	if err != nil {
		return "", errors.New("invalid configured SP path")
	}
	// Native URI normalization sorts parameter pairs without dropping duplicates.
	if u.RawQuery != "" {
		pairs := strings.Split(u.RawQuery, "&")
		sort.Strings(pairs)
		u.RawQuery = strings.Join(pairs, "&")
	}
	return u.String(), nil
}

func checkIdPDomain(c Config, host string, info idpConfig) error {
	if c.SAML.Domains != nil && (info.Extra.Domain == nil || host == "" || *info.Extra.Domain != host) {
		return &apiError{404, "not-found", "Could not find IdP: " + info.ID.String()}
	}
	return nil
}

func authRequestFormat(query url.Values) (verdictFormat, error) {
	var format verdictFormat
	first := func(name string) *string {
		if values, ok := query[name]; ok && len(values) > 0 {
			value := values[0]
			return &value
		}
		return nil
	}
	format.Success, format.Error, format.Label = first("success_redirect"), first("error_redirect"), first("label")
	fail := func(label string) (verdictFormat, error) { return verdictFormat{}, &apiError{400, label, label} }
	if format.Success == nil && format.Error == nil {
		return format, nil
	}
	if format.Success == nil || format.Error == nil {
		return fail("need-both-redirect-urls")
	}
	for _, raw := range []*string{format.Success, format.Error} {
		u, err := url.Parse(*raw)
		if err != nil || !u.IsAbs() {
			return verdictFormat{}, &apiError{400, "bad-request", "Invalid redirect URI"}
		}
		scheme, _, _ := strings.Cut(*raw, ":")
		if !strings.HasPrefix(scheme, "wire") {
			return fail("invalid-schema")
		}
		if len(u.String()) > 140 {
			return fail("url-too-long")
		}
	}
	format.Mobile = true
	return format, nil
}

func authRequestXML(id, issuer string, now time.Time) string {
	return `<?xml version="1.0" encoding="UTF-8"?><AuthnRequest` + nativeNamespaces + ` ID="` + nativeXMLEscape(id, true) + `" IssueInstant="` + now.UTC().Format(time.RFC3339Nano) + `" Version="2.0" xmlns="` + protocolNS + `"><Issuer xmlns="` + assertionNS + `">` + nativeXMLEscape(issuer, false) + `</Issuer><NameIDPolicy AllowCreate="true" Format="` + nameID11 + `unspecified"/></AuthnRequest>`
}

func samlHTML(body string) string {
	return `<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.1//EN" "http://www.w3.org/TR/xhtml11/DTD/xhtml11.dtd"><html xml:lang="en" xmlns="http://www.w3.org/1999/xhtml">` + body + `</html>`
}

func authRequestForm(destination, request string) string {
	encoded := base64.StdEncoding.EncodeToString([]byte(request))
	return samlHTML(`<body onload="document.forms[0].submit()"><noscript><p><strong>Note:</strong> Since your browser does not support JavaScript, you must press the Continue button once to proceed.</p></noscript><form action="` + nativeXMLEscape(destination, true) + `" method="post" accept-charset="utf-8"><input type="hidden" name="SAMLRequest" value="` + encoded + `"/><noscript><input type="submit" value="Continue"/></noscript></form></body>`)
}

func metadataXML(id, uri string, now time.Time, contacts []map[string]any) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<?xml version="1.0" encoding="UTF-8"?><EntityDescriptor%s entityID="%s" xmlns="%s"><SPSSODescriptor AuthnRequestsSigned="false" ID="%s" WantAssertionsSigned="true" cacheDuration="P30D" protocolSupportEnumeration="%s" validUntil="%s"><Organization><OrganizationName xml:lang="EN">spar</OrganizationName><OrganizationDisplayName xml:lang="EN">spar</OrganizationDisplayName><OrganizationURL xml:lang="EN">%s</OrganizationURL></Organization>`, nativeNamespaces, nativeXMLEscape(uri, true), metadataNS, id, protocolNS, now.Add(365*24*time.Hour).UTC().Format(time.RFC3339Nano), nativeXMLEscape(uri, false))
	for _, contact := range contacts {
		typeName, _ := contact["type"].(string)
		typeName = strings.ToLower(strings.TrimPrefix(typeName, "Contact"))
		b.WriteString(`<ContactPerson contactType="` + typeName + `">`)
		for _, field := range []struct{ key, element string }{{"company", "Company"}, {"givenName", "GivenName"}, {"surname", "SurName"}, {"email", "EmailAddress"}, {"phone", "TelephoneNumber"}} {
			if value, ok := contact[field.key].(string); ok {
				b.WriteString("<" + field.element + ">" + nativeXMLEscape(value, false) + "</" + field.element + ">")
			}
		}
		b.WriteString("</ContactPerson>")
	}
	fmt.Fprintf(&b, `<NameIDFormat>%sunspecified</NameIDFormat><NameIDFormat>%sentity</NameIDFormat><AssertionConsumerService Binding="%s" Location="%s" index="0" isDefault="true"/></SPSSODescriptor></EntityDescriptor>`, nameID11, nameID20, postBinding, nativeXMLEscape(uri, true))
	return b.String()
}

func serveSAMLRequest(w http.ResponseWriter, r *http.Request, c Config, store samlRequestStore) bool {
	metadata := r.URL.Path == "/sso/metadata" || strings.HasPrefix(r.URL.Path, "/sso/metadata/")
	initiate := strings.HasPrefix(r.URL.Path, "/sso/initiate-login/")
	if !metadata && !initiate {
		return false
	}
	fail := func(err error) bool { domainError(w, idpError(err)); return true }
	if r.Method != "GET" && r.Method != "HEAD" {
		w.WriteHeader(405)
		return true
	}
	host := r.Header.Get("Z-Host")
	if metadata {
		var team *gocql.UUID
		if r.URL.Path != "/sso/metadata" {
			text := strings.TrimPrefix(r.URL.Path, "/sso/metadata/")
			if strings.Contains(text, "/") {
				return false
			}
			id, err := gocql.ParseUUID(text)
			if err != nil {
				return fail(&apiError{400, "bad-request", "Invalid team ID"})
			}
			team = &id
		}
		uri, err := responseURI(c, host, team)
		if err != nil {
			return fail(err)
		}
		id, err := gocql.RandomUUID()
		if err != nil {
			return fail(err)
		}
		d, _ := domainConfig(c, host)
		w.Header().Set("Content-Type", "application/xml")
		if r.Method == "GET" {
			_, _ = w.Write([]byte(metadataXML("_"+id.String(), uri, time.Now(), d.Contacts)))
		} else {
			w.WriteHeader(200)
		}
		return true
	}
	text := strings.TrimPrefix(r.URL.Path, "/sso/initiate-login/")
	if strings.Contains(text, "/") {
		return false
	}
	id, err := gocql.ParseUUID(text)
	if err != nil {
		return fail(&apiError{400, "bad-request", "Invalid IdP ID"})
	}
	format, err := authRequestFormat(r.URL.Query())
	if err != nil {
		return fail(err)
	}
	info, err := store.getIdP(r.Context(), id)
	if err != nil {
		return fail(err)
	}
	if err = checkIdPDomain(c, host, *info); err != nil {
		return fail(err)
	}
	if r.Method == "HEAD" {
		w.Header().Set("Content-Type", "text/plain;charset=utf-8")
		w.WriteHeader(200)
		return true
	}
	var team *gocql.UUID
	if info.version() == 2 {
		team = &info.Extra.Team
	}
	uri, err := responseURI(c, host, team)
	if err != nil {
		return fail(err)
	}
	// The native SAML interpreter reloads IdP config inside authreq after the
	// API domain/version precheck. Preserve that effect order.
	info, err = store.getIdP(r.Context(), id)
	if err != nil {
		return fail(err)
	}
	requestID, err := gocql.RandomUUID()
	if err != nil {
		return fail(err)
	}
	reqID, now := "_"+requestID.String(), time.Now()
	seconds := float64(*c.MaxAuthRequestTTL)
	end := now.Add(time.Duration(*c.MaxAuthRequestTTL) * time.Second)
	if err = store.storeAuthRequest(r.Context(), reqID, info.Metadata.Issuer, time.Now(), end, *c.MaxAuthRequestTTL); err != nil {
		return fail(err)
	}
	if err = store.storeVerdict(r.Context(), reqID, seconds, format); err != nil {
		return fail(err)
	}
	w.Header().Set("Content-Type", "text/html")
	_, _ = w.Write([]byte(authRequestForm(info.Metadata.RequestURI, authRequestXML(reqID, uri, now))))
	return true
}
