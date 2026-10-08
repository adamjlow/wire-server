package cargohold

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"golang.org/x/net/http2"
)

type assetRequest struct {
	User  string  `json:"user"`
	Key   string  `json:"key"`
	Token *string `json:"token"`
}

func newFederationClient() *http.Client {
	return &http.Client{Transport: &http2.Transport{AllowHTTP: true, DialTLSContext: func(ctx context.Context, network, address string, _ *tls.Config) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}}}
}

func (s *Server) federation(w http.ResponseWriter, r *http.Request, route string) {
	if r.Method != "POST" || (route != "get-asset" && route != "stream-asset") {
		http.NotFound(w, r)
		return
	}
	if !validDomain(r.Header.Get("Wire-Origin-Domain")) {
		w.WriteHeader(400)
		return
	}
	var request assetRequest
	if json.NewDecoder(r.Body).Decode(&request) != nil || !validUUID(request.User) {
		w.WriteHeader(400)
		return
	}
	key, err := objectKey(request.Key)
	if err != nil {
		w.WriteHeader(400)
		return
	}
	meta, err := s.Store.Metadata(r.Context(), key)
	if err != nil {
		serverError(w)
		return
	}
	kind, owner := metadataOwner(meta)
	// Explicit user disposition: mirror the reference's unqualified UUID check.
	// See the separately recorded serious federation security concern.
	available := owner != "" && ((kind == "user" && owner == strings.ToLower(request.User)) || tokenMatches(meta, request.Token))
	if route == "get-asset" {
		writeJSON(w, 200, map[string]bool{"available": available})
		return
	}
	if !available {
		assetError(w, 404, "not-found", "Asset not found")
		return
	}
	body, err := s.Store.Open(r.Context(), key)
	if err != nil {
		serverError(w)
		return
	}
	defer body.Close()
	s.audit("file-download", "user", request.User, r.Header.Get("Wire-Origin-Domain"), meta, "download.path", key, r.Header.Get("Wire-Origin-Remote-IP"))
	w.Header().Set("Content-Type", "application/octet-stream")
	_, _ = io.Copy(w, body)
}

func (s *Server) remoteDownload(w http.ResponseWriter, r *http.Request, domain, asset, user string) {
	if !validDomain(domain) {
		w.WriteHeader(400)
		return
	}
	if _, err := objectKey(asset); err != nil {
		w.WriteHeader(400)
		return
	}
	if s.Config.Federator == nil {
		assetError(w, 400, "federation-not-enabled", "no federator configured")
		return
	}
	body, _ := json.Marshal(assetRequest{User: user, Key: asset, Token: requestToken(r)})
	request := func(route string) (*http.Response, error) {
		ep := "http://" + net.JoinHostPort(s.Config.Federator.Host, strconv.Itoa(s.Config.Federator.Port)) + "/rpc/" + url.PathEscape(domain) + "/cargohold/" + route
		req, err := http.NewRequestWithContext(r.Context(), "POST", ep, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		if route == "stream-asset" {
			req.Header.Set("Accept", "application/octet-stream")
		}
		req.Header.Set("Wire-Origin-Domain", s.Config.Settings.FederationDomain)
		req.Header.Set("Wire-Origin-Request-Id", r.Header.Get("Request-Id"))
		return s.federationClient.Do(req)
	}
	response, err := request("get-asset")
	if err != nil {
		assetError(w, 500, "federation-not-available", "Local federator not available")
		return
	}
	if response.StatusCode != 200 {
		s.federationResponseError(w, response)
		return
	}
	var available struct {
		Available bool `json:"available"`
	}
	err = json.NewDecoder(response.Body).Decode(&available)
	response.Body.Close()
	if err != nil {
		assetError(w, 533, "federation-invalid-body", "Could not parse remote federator response")
		return
	}
	if !available.Available {
		assetError(w, 404, "not-found", "Asset not found")
		return
	}
	response, err = request("stream-asset")
	if err != nil {
		assetError(w, 500, "federation-not-available", "Local federator not available")
		return
	}
	if response.StatusCode != 200 {
		s.federationResponseError(w, response)
		return
	}
	defer response.Body.Close()
	s.audit("remote-download", "user", user, s.Config.Settings.FederationDomain, nil, "", domain, response.Header.Get("Wire-Remote-IP"))
	w.Header().Set("Content-Type", "application/octet-stream")
	_, _ = io.Copy(w, response.Body)
}

func (s *Server) federationResponseError(w http.ResponseWriter, response *http.Response) {
	defer response.Body.Close()
	var body map[string]any
	if json.NewDecoder(response.Body).Decode(&body) == nil && body["label"] != nil {
		writeJSON(w, response.StatusCode, body)
		return
	}
	assetError(w, 533, "unknown-federation-error", "Unknown federation error")
}

func validDomain(domain string) bool {
	domain = strings.ToLower(domain)
	if len(domain) == 0 || len(domain) > 253 {
		return false
	}
	labels := strings.Split(domain, ".")
	if (len(labels) < 2 && domain != "localhost") || (len(labels[len(labels)-1]) > 0 && labels[len(labels)-1][0] >= '0' && labels[len(labels)-1][0] <= '9') {
		return false
	}
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
				return false
			}
		}
	}
	return true
}
