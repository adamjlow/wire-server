// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/apache/cassandra-gocql-driver/v2"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

func idpError(err error) error {
	if errors.Is(err, errIdPNotFound) {
		return &apiError{404, "not-found", "Could not find IdP."}
	}
	return err
}
func checkIdPCertificates(c Config, metadata idpMetadata) error {
	for _, cert := range metadata.Certificates {
		digest := sha1.Sum(cert.Raw)
		fingerprint := hex.EncodeToString(digest[:])
		allowed := false
		for _, configured := range c.Fingerprints {
			normalized := strings.Join(strings.Fields(strings.ReplaceAll(configured, ":", "")), "")
			if strings.EqualFold(normalized, fingerprint) {
				allowed = true
				break
			}
		}
		if !allowed && (len(c.Fingerprints) > 0 || c.SAML.Domains != nil) {
			parts := make([]string, 20)
			for i := range parts {
				parts[i] = strings.ToUpper(fingerprint[i*2 : i*2+2])
			}
			return &apiError{403, "idp-cert-not-allowed", "IdP certificate not in the configured allowlist: " + strings.Join(parts, ":")}
		}
	}
	return nil
}
func idpDomain(c Config, host string) *string {
	if _, ok := c.SAML.Domains[host]; ok {
		value := host
		return &value
	}
	return nil
}
func readIdPMetadata(r *http.Request) (string, idpMetadata, error) {
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		return "", idpMetadata{}, &apiError{415, "bad-request", "Unsupported media type"}
	}
	var raw string
	switch contentType {
	case "application/xml":
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return "", idpMetadata{}, err
		}
		raw = strings.ToValidUTF8(string(body), "\ufffd")
	case "application/json":
		object, err := decodeObject(r)
		if err != nil {
			return "", idpMetadata{}, err
		}
		if json.Unmarshal(object["value"], &raw) != nil {
			return "", idpMetadata{}, &apiError{400, "bad-request", "Invalid IdP metadata"}
		}
	default:
		return "", idpMetadata{}, &apiError{415, "bad-request", "Unsupported media type"}
	}
	metadata, err := parseIdPMetadata(raw)
	if err != nil {
		return "", metadata, &apiError{400, "bad-request", "Invalid IdP metadata"}
	}
	return raw, metadata, nil
}
func httpsIdP(metadata idpMetadata) error {
	uri, err := url.Parse(metadata.RequestURI)
	if err != nil || uri.Scheme != "https" {
		return &apiError{400, "idp-must-be-https", "an idp request uri must be https, not http or other: " + metadata.RequestURI}
	}
	return nil
}
func serveIdPs(w http.ResponseWriter, r *http.Request, c Config, s cassandraStore, p peerClient) bool {
	const base = "/identity-providers"
	internal := strings.HasPrefix(r.URL.Path, "/i/identity-providers/")
	if !internal && r.URL.Path != base && !strings.HasPrefix(r.URL.Path, base+"/") {
		return false
	}
	user, requestID := r.Header.Get("Z-User"), r.Header.Get("Request-Id")
	fail := func(err error) bool { domainError(w, idpError(err)); return true }
	if internal {
		id, err := gocql.ParseUUID(strings.TrimPrefix(r.URL.Path, "/i/identity-providers/"))
		if err != nil {
			return fail(&apiError{400, "bad-request", "Invalid team ID"})
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			w.WriteHeader(405)
			return true
		}
		infos, err := s.idpsByTeam(r.Context(), id)
		if err != nil {
			return fail(err)
		}
		jsonResponse(w, r, map[string]any{"providers": infos})
		return true
	}
	suffix := strings.TrimPrefix(r.URL.Path, base)
	var current *idpConfig
	var id gocql.UUID
	var err error
	rawRoute := strings.HasSuffix(suffix, "/raw")
	idText := strings.TrimSuffix(strings.TrimPrefix(suffix, "/"), "/raw")
	if suffix != "" {
		if strings.Contains(idText, "/") {
			return false
		}
		id, err = gocql.ParseUUID(idText)
		if err != nil {
			return fail(&apiError{400, "bad-request", "Invalid IdP ID"})
		}
	}
	if r.Method == "DELETE" {
		return false
	} // Bound-user/purge behavior awaits the SAML user store.
	if r.Method == "GET" || r.Method == "HEAD" {
		if suffix == "" {
			team, err := p.authorize(r.Context(), user, requestID)
			if err != nil {
				var api *apiError
				if errors.As(err, &api) && api.Label == "insufficient-permissions" {
					err = permissionError("ReadIdp")
				}
				return fail(err)
			}
			infos, err := s.idpsByTeam(r.Context(), team)
			if err != nil {
				return fail(err)
			}
			jsonResponse(w, r, map[string]any{"providers": infos})
			return true
		}
		current, err = s.getIdP(r.Context(), id)
		if err != nil {
			return fail(err)
		}
		if err = p.idpPermission(r.Context(), user, requestID, current.Extra.Team); err != nil {
			return fail(err)
		}
		if rawRoute {
			raw, err := s.rawIdP(r.Context(), id)
			if err != nil {
				return fail(err)
			}
			if raw == nil {
				return fail(&apiError{404, "not-found", "Could not find IdP: " + id.String()})
			}
			w.Header().Set("Content-Type", "application/xml")
			if r.Method != "HEAD" {
				_, _ = io.WriteString(w, *raw)
			}
			return true
		}
		jsonResponse(w, r, current)
		return true
	}
	if rawRoute || r.Method != "POST" && r.Method != "PUT" || r.Method == "POST" && suffix != "" || r.Method == "PUT" && suffix == "" {
		w.WriteHeader(405)
		return true
	}
	var team gocql.UUID
	if r.Method == "POST" {
		team, err = p.idpAdmin(r.Context(), user, requestID)
		if err != nil {
			return fail(err)
		}
	}
	raw, metadata, err := readIdPMetadata(r)
	if err != nil {
		return fail(err)
	}
	var handle *string
	if values, ok := r.URL.Query()["handle"]; ok {
		value := ""
		if len(values) > 0 {
			value = values[0]
		}
		if utf8.RuneCountInString(value) < 1 || utf8.RuneCountInString(value) > 32 {
			return fail(&apiError{400, "bad-request", "Invalid IdP handle"})
		}
		handle = &value
	}
	host := idpDomain(c, r.Header.Get("Z-Host"))
	version, _ := strconv.Atoi(r.Header.Get("X-Wire-API-Version"))
	legacy := version < 8
	var info idpConfig
	var replacing *gocql.UUID
	if r.Method == "POST" {
		if legacy {
			tokens, err := s.teamTokens(r.Context(), team)
			if err != nil {
				return fail(err)
			}
			existing, err := s.idpsByTeam(r.Context(), team)
			if err != nil {
				return fail(err)
			}
			if len(tokens) > 0 && len(existing) > 0 {
				return fail(&apiError{400, "more-than-one-idp", "Team can have at most one IdP configured: teams with SCIM tokens can only have at most one IdP"})
			}
			host = nil
		}
		apiVersion := "v1"
		if value := r.URL.Query().Get("api_version"); value != "" {
			if value != "v1" && value != "v2" {
				return fail(&apiError{400, "bad-request", "Invalid IdP API version"})
			}
			apiVersion = value
		}
		if text := r.URL.Query().Get("replaces"); text != "" {
			parsed, err := gocql.ParseUUID(text)
			if err != nil {
				return fail(&apiError{400, "bad-request", "Invalid replacement ID"})
			}
			replacing = &parsed
		}
		if err = p.ssoEnabled(r.Context(), team, requestID); err != nil {
			return fail(err)
		}
		if host != nil {
			existing, err := s.idpsByTeam(r.Context(), team)
			if err != nil {
				return fail(err)
			}
			for _, other := range existing {
				if other.Extra.Domain != nil && *other.Extra.Domain == *host {
					return fail(&apiError{409, "idp-duplicate-domain-for-team", "This team already has an IdP configured for this domain."})
				}
			}
		}
		if err = checkIdPCertificates(c, metadata); err != nil {
			return fail(err)
		}
		if handle == nil {
			handles, err := s.idpHandles(r.Context(), team)
			if err != nil {
				return fail(err)
			}
			value := uniqueIdPHandle(handles)
			handle = &value
		}
		id, err = gocql.RandomUUID()
		if err != nil {
			return fail(err)
		}
		info = idpConfig{ID: id, Metadata: metadata, Extra: idpExtra{Team: team, Version: &apiVersion, OldIssuers: []string{}, Handle: *handle, Domain: host}}
		if replacing != nil {
			old, err := s.getIdP(r.Context(), *replacing)
			if err != nil {
				return fail(err)
			}
			info.Extra.OldIssuers = append([]string{old.Metadata.Issuer}, old.Extra.OldIssuers...)
		}
		if err = httpsIdP(metadata); err != nil {
			return fail(err)
		}
		existing, err := s.idpByIssuer(r.Context(), metadata.Issuer, info.version(), team)
		if err != nil {
			return fail(err)
		}
		if existing != nil {
			message := "you can't create an IdP with api_version v1 if the issuer is already in use on the wire instance."
			if info.version() == 2 {
				message = "you can't create an IdP with api_version v1 if the issuer is already in use in your team."
			}
			return fail(&apiError{400, "idp-already-in-use", message})
		}
	} else {
		current, err = s.getIdP(r.Context(), id)
		if err != nil {
			return fail(err)
		}
		team = current.Extra.Team
		if err = p.idpPermission(r.Context(), user, requestID, team); err != nil {
			return fail(err)
		}
		info = *current
		info.Metadata = metadata
		if current.Metadata.Issuer != metadata.Issuer {
			existing, err := s.idpByIssuer(r.Context(), metadata.Issuer, info.version(), team)
			if err != nil {
				return fail(err)
			}
			if existing != nil && existing.ID != id {
				return fail(&apiError{400, "idp-issuer-in-use", "The issuer of your IdP is already in use.  Remove the entry in the team that uses it, or construct a new IdP issuer."})
			}
			old := []string{}
			seen := map[string]bool{metadata.Issuer: true}
			for _, issuer := range append([]string{current.Metadata.Issuer}, current.Extra.OldIssuers...) {
				if !seen[issuer] {
					old = append(old, issuer)
					seen[issuer] = true
				}
			}
			info.Extra.OldIssuers = old
		}
		if err = httpsIdP(metadata); err != nil {
			return fail(err)
		}
		if err = p.ssoEnabled(r.Context(), team, requestID); err != nil {
			return fail(err)
		}
		if err = checkIdPCertificates(c, metadata); err != nil {
			return fail(err)
		}
		if host != nil {
			existing, err := s.idpsByTeam(r.Context(), team)
			if err != nil {
				return fail(err)
			}
			for _, other := range existing {
				if other.ID != id && other.Extra.Domain != nil && *other.Extra.Domain == *host {
					return fail(&apiError{409, "idp-duplicate-domain-for-team", "This team already has an IdP configured for this domain."})
				}
			}
		}
		if handle != nil {
			info.Extra.Handle = *handle
		}
		info.Extra.Domain = host
	}
	if err = s.storeRawIdP(r.Context(), id, raw); err != nil {
		return fail(err)
	}
	if err = s.insertIdP(r.Context(), info); err != nil {
		return fail(err)
	}
	if r.Method == "POST" && replacing != nil {
		if err = s.setIdPReplacement(r.Context(), *replacing, &id); err != nil {
			return fail(err)
		}
	}
	if r.Method == "PUT" {
		var scope *gocql.UUID
		if info.version() == 2 {
			scope = &team
		}
		for _, issuer := range info.Extra.OldIssuers {
			if err = s.deleteIdPIssuer(r.Context(), issuer, scope); err != nil {
				return fail(err)
			}
		}
	}
	if c.SAML.Domains != nil {
		tag := "created"
		if r.Method == "PUT" {
			tag = "updated"
		}
		if err = p.notifyIdP(r.Context(), tag, user, requestID, current, &info); err != nil {
			return fail(err)
		}
	}
	if r.Method == "POST" {
		w.Header().Set("Content-Type", "application/json;charset=utf-8")
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(info)
	} else {
		jsonResponse(w, r, info)
	}
	return true
}
