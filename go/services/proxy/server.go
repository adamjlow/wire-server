// SPDX-License-Identifier: AGPL-3.0-or-later
package proxy

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type handler struct {
	config  Config
	secrets *Secrets
	client  *http.Client
	stream  *http.Client
}

func NewHandler(config Config, secrets *Secrets) http.Handler {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ForceAttemptHTTP2 = false
	// Cloning DefaultTransport can retain its initialized HTTP/2 ALPN settings.
	// Explicit protocol selection keeps negotiation and request encoding aligned
	// with the pinned reference's HTTP/1.1 provider requests.
	transport.Protocols = new(http.Protocols)
	transport.Protocols.SetHTTP1(true)
	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = new(tls.Config)
	}
	transport.TLSClientConfig.NextProtos = []string{"http/1.1"}
	transport.ResponseHeaderTimeout = 5 * time.Second
	transport.MaxIdleConnsPerHost = config.HTTPPoolSize
	transport.MaxIdleConns = 3 * config.HTTPPoolSize
	return newHandler(config, secrets, transport)
}

func newHandler(config Config, secrets *Secrets, transport http.RoundTripper) *handler {
	return &handler{config: config, secrets: secrets,
		client: &http.Client{Transport: transport},
		stream: &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func writeError(w http.ResponseWriter, status int, label, message string) {
	if status == 500 {
		message = "Internal Server Error"
	}
	w.Header().Set("Content-Type", "application/json")
	b, _ := json.Marshal(struct {
		Code    int    `json:"code"`
		Label   string `json:"label"`
		Message string `json:"message"`
	}{status, label, message})
	w.WriteHeader(status)
	_, _ = w.Write(b)
}

func internalError(w http.ResponseWriter) {
	writeError(w, 500, "internal-error", "Internal Server Error")
}

func unsniffed(w http.ResponseWriter, status int, body []byte) {
	// Warp's empty/parser responses do not acquire Go's automatic sniffed type.
	w.Header()["Content-Type"] = nil
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.EscapedPath()
	segments := strings.Split(strings.TrimPrefix(raw, "/"), "/")
	first, _ := url.PathUnescape(segments[0])
	if strings.HasPrefix(first, "v") && decimalRunes(first[1:]) {
		if len(segments) > 1 {
			internal, _ := url.PathUnescape(segments[1])
			if internal == "i" || internal == "api-internal" {
				writeError(w, 404, "unsupported-version", "Internal APIs (`/i/...`) are not under version control")
				return
			}
		}
		disabled := false
		for _, version := range h.config.DisabledAPIVersions {
			if version == first || (version == "development" && first == "v19") {
				disabled = true
			}
		}
		if !knownVersion(first) || disabled {
			writeError(w, 404, "unsupported-version", "Version "+first+" is not supported")
			return
		}
		segments = segments[1:]
		raw = "/" + strings.Join(segments, "/")
	}
	path, err := url.PathUnescape(raw)
	if err != nil {
		unsniffed(w, 400, nil)
		return
	}
	if path == "/i/status" {
		if r.Method != "GET" && r.Method != "HEAD" {
			unsniffed(w, 405, nil)
			return
		}
		unsniffed(w, 200, nil)
		return
	}
	if strings.EqualFold(r.Header.Get("Content-Encoding"), "gzip") {
		body, err := gzip.NewReader(r.Body)
		if err != nil {
			internalError(w)
			return
		}
		defer body.Close()
		r.Body = body
	}
	for _, route := range []struct {
		prefix, outgoing, secret string
		key                      string
		endpoint                 *Endpoint
		fallback                 Endpoint
		static                   bool
	}{
		{"/proxy/giphy/v1/gifs", "/v1/gifs", "giphy", "api_key", h.config.GiphyEndpoint, Endpoint{"api.giphy.com", 443}, false},
		{"/proxy/youtube/v3", "/youtube/v3", "youtube", "key", h.config.YoutubeEndpoint, Endpoint{"www.googleapis.com", 443}, false},
		{"/proxy/googlemaps/api/staticmap", "/maps/api/staticmap", "googlemaps", "key", h.config.GoogleMapsEndpoint, Endpoint{"www.googleapis.com", 443}, true},
		{"/proxy/googlemaps/maps/api/geocode", "/maps/api/geocode", "googlemaps", "key", h.config.GoogleMapsEndpoint, Endpoint{"www.googleapis.com", 443}, false},
	} {
		if path != route.prefix && (route.static || !strings.HasPrefix(path, route.prefix+"/")) {
			continue
		}
		if !strings.EqualFold(r.Method, "GET") {
			writeError(w, 405, "method-not-allowed", "Method not allowed")
			return
		}
		secret, ok := h.secrets.get(route.secret)
		if !ok {
			internalError(w)
			return
		}
		outgoing := route.outgoing
		if !route.static {
			if index := strings.Index(raw, route.outgoing); index >= 0 {
				outgoing = raw[index:]
			} else {
				outgoing = "/"
			}
		}
		endpoint := route.fallback
		if route.endpoint != nil {
			endpoint = *route.endpoint
		}
		h.reverse(w, r, h.endpointURL(endpoint)+outgoing+"?"+providerQuery(r.URL.RawQuery, route.key, secret))
		return
	}
	switch path {
	case "/proxy/spotify/api/token":
		h.spotify(w, r)
	case "/proxy/soundcloud/resolve", "/proxy/soundcloud/stream":
		var target string
		present := false
		for _, pair := range parseQuery(r.URL.RawQuery) {
			if pair.key == "url" {
				present = true
				if pair.value != nil {
					target = *pair.value
				}
				break
			}
		}
		if !present {
			unsniffed(w, 400, []byte("Query parameter url is required"))
			return
		}
		h.soundcloud(w, r, target, strings.HasSuffix(path, "/stream"))
	default:
		unsniffed(w, 404, nil)
	}
}

func decimalRunes(value string) bool {
	for _, c := range value {
		if !unicode.IsDigit(c) {
			return false
		}
	}
	return true
}

func (h *handler) endpointURL(endpoint Endpoint) string {
	scheme := "https"
	if h.config.DisableTLSForTest != nil && *h.config.DisableTLSForTest {
		scheme = "http"
	}
	return scheme + "://" + net.JoinHostPort(endpoint.Host, strconv.Itoa(endpoint.Port))
}

func requestFor(ctx context.Context, method, target string, body []byte) (*http.Request, error) {
	r, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if err == nil {
		// Haskell does not add Go's default User-Agent.
		r.Header["User-Agent"] = []string{""}
	}
	return r, err
}

func pause(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (h *handler) reverse(w http.ResponseWriter, incoming *http.Request, target string) {
	for attempt := 0; attempt < 3; attempt++ {
		r, err := requestFor(incoming.Context(), incoming.Method, target, nil)
		if err != nil {
			writeError(w, 502, "bad-gateway", "Bad gateway")
			return
		}
		r.Header.Set("X-Real-IP", "0.0.0.0")
		if incoming.Method != "GET" {
			// http-client sends an explicit zero length for the unusual mixed-case
			// methods accepted by proxyServant's case-insensitive GET check.
			r.TransferEncoding = []string{"identity"}
		}
		response, err := h.client.Do(r)
		if err != nil {
			if attempt < 2 && pause(incoming.Context(), 5*time.Millisecond) {
				continue
			}
			writeError(w, 502, "bad-gateway", "Bad gateway")
			return
		}
		if response.StatusCode == 502 && attempt < 2 {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			if pause(incoming.Context(), 5*time.Millisecond) {
				continue
			}
			return
		}
		defer response.Body.Close()
		if response.StatusCode >= 500 {
			body, err := io.ReadAll(response.Body)
			if err != nil {
				internalError(w)
				return
			}
			writeUpstreamError(w, response.StatusCode, body)
			return
		}
		for name, values := range response.Header {
			for _, value := range values {
				w.Header().Add(name, value)
			}
		}
		if w.Header().Get("Content-Type") == "" {
			w.Header()["Content-Type"] = nil
		}
		w.WriteHeader(response.StatusCode)
		_, _ = io.Copy(w, response.Body)
		return
	}
}

func retryable(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var operation *net.OpError
	return errors.As(err, &operation) && (operation.Op == "dial" || !operation.Timeout())
}

func (h *handler) buffered(ctx context.Context, client *http.Client, method, target string, body []byte, headers http.Header) (*http.Response, []byte, error) {
	for attempt := 0; ; attempt++ {
		r, err := requestFor(ctx, method, target, body)
		if err != nil {
			return nil, nil, err
		}
		for name, values := range headers {
			r.Header[name] = append([]string{}, values...)
		}
		response, err := client.Do(r)
		if err != nil {
			if attempt < 2 && retryable(err) && pause(ctx, time.Duration(5<<attempt)*time.Millisecond) {
				continue
			}
			return nil, nil, err
		}
		payload, err := io.ReadAll(response.Body)
		_ = response.Body.Close()
		return response, payload, err
	}
}

func plainUpstream(w http.ResponseWriter, response *http.Response, body []byte) {
	if response.StatusCode >= 500 {
		writeUpstreamError(w, response.StatusCode, body)
		return
	}
	if values := response.Header.Values("Content-Type"); len(values) > 0 {
		w.Header().Add("Content-Type", values[0])
	}
	w.Header().Add("Content-Type", "text/plain; charset=UTF-8")
	w.WriteHeader(response.StatusCode)
	_, _ = w.Write(body)
}

func writeUpstreamError(w http.ResponseWriter, status int, body []byte) {
	// catchErrors preserves a serialized Wai.Error; other 5xx bodies become its
	// message and lose upstream headers. Invalid UTF-8 is decoded leniently.
	var existing struct {
		Code    *int    `json:"code"`
		Label   *string `json:"label"`
		Message *string `json:"message"`
	}
	if json.Unmarshal(body, &existing) == nil && existing.Code != nil && existing.Label != nil && existing.Message != nil {
		writeError(w, *existing.Code, *existing.Label, *existing.Message)
		return
	}
	writeError(w, status, "server-error", strings.ToValidUTF8(string(body), "\uFFFD"))
}

func (h *handler) spotify(w http.ResponseWriter, r *http.Request) {
	secret, ok := h.secrets.get("spotify")
	if !ok {
		internalError(w)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		internalError(w)
		return
	}
	endpoint := Endpoint{"accounts.spotify.com", 443}
	if h.config.SpotifyEndpoint != nil {
		endpoint = *h.config.SpotifyEndpoint
	}
	headers := http.Header{"Authorization": {secret}}
	for _, name := range []string{"Accept", "Content-Type"} {
		if values := r.Header.Values(name); len(values) > 0 {
			headers[name] = values
		}
	}
	response, payload, err := h.buffered(r.Context(), h.client, "POST", h.endpointURL(endpoint)+"/api/token", body, headers)
	if err != nil {
		internalError(w)
		return
	}
	plainUpstream(w, response, payload)
}

func (h *handler) soundcloud(w http.ResponseWriter, r *http.Request, target string, stream bool) {
	secret, ok := h.secrets.get("soundcloud")
	if !ok {
		internalError(w)
		return
	}
	endpoint := Endpoint{"api.soundcloud.com", 443}
	if h.config.SoundcloudEndpoint != nil {
		endpoint = *h.config.SoundcloudEndpoint
	}
	if !stream {
		response, body, err := h.buffered(r.Context(), h.client, "GET", h.endpointURL(endpoint)+"/resolve?url="+encodeQueryBytes(target)+"&client_id="+encodeQueryBytes(secret), nil, nil)
		if err != nil {
			internalError(w)
			return
		}
		plainUpstream(w, response, body)
		return
	}
	u, err := url.Parse(target)
	allowHTTP := h.config.DisableTLSForTest != nil && *h.config.DisableTLSForTest
	if err != nil || u.Hostname() != endpoint.Host || (!allowHTTP && u.Scheme != "https") {
		internalError(w)
		return
	}
	if u.RawQuery != "" {
		u.RawQuery += "&"
	}
	u.RawQuery += "client_id=" + encodeQueryBytes(secret)
	response, _, err := h.buffered(r.Context(), h.stream, "GET", u.String(), nil, nil)
	if err != nil || response.StatusCode != 302 || len(response.Header.Values("Location")) == 0 {
		internalError(w)
		return
	}
	w.Header().Set("Location", response.Header.Values("Location")[0])
	w.Header().Set("Content-Type", "text/plain; charset=UTF-8")
	w.WriteHeader(302)
}
