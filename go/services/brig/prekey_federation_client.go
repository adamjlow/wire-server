// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"mime"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type prekeyFederationFailure struct {
	code           int
	label, message string
	data, inner    any
}

func (e prekeyFederationFailure) Error() string {
	return "prekey federation request failed: " + e.label
}
func (e prekeyFederationFailure) wire() map[string]any {
	result := map[string]any{"code": e.code, "label": e.label, "message": e.message}
	if e.data != nil {
		result["data"] = e.data
	}
	if e.inner != nil {
		result["inner"] = e.inner
	}
	return result
}

type prekeyFederationClient struct {
	client   *http.Client
	endpoint *Endpoint
	origin   string
}

func newPrekeyFederationClient(c Config) (prekeyFederationClient, func()) {
	protocols := new(http.Protocols)
	protocols.SetUnencryptedHTTP2(true)
	transport := &http.Transport{Proxy: nil, Protocols: protocols, DialContext: (&net.Dialer{Timeout: 10 * time.Second}).DialContext, MaxIdleConns: 4096, MaxIdleConnsPerHost: 1024, DisableCompression: true}
	return prekeyFederationClient{client: &http.Client{Transport: transport, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, endpoint: c.Federator, origin: c.Settings.Domain}, transport.CloseIdleConnections
}

func federationJSON(raw []byte) (json.RawMessage, error) {
	value, err := parsePropertyValue(raw)
	if err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func federationInteger(raw json.RawMessage) (int64, error) {
	match := registrationNumberPattern.FindStringSubmatch(string(bytes.TrimSpace(raw)))
	if match == nil {
		return 0, errors.New("invalid federation integer")
	}
	digits := strings.TrimLeft(match[2]+match[3], "0")
	if digits == "" {
		return 0, nil
	}
	trimmed := strings.TrimRight(digits, "0")
	power := big.NewInt(int64(len(digits) - len(trimmed) - len(match[3])))
	if match[4] != "" {
		exponent, ok := new(big.Int).SetString(strings.TrimPrefix(match[4], "+"), 10)
		if !ok {
			return 0, errors.New("invalid federation integer")
		}
		power.Add(power, exponent)
	}
	if power.Sign() < 0 || power.Cmp(big.NewInt(19)) > 0 || len(trimmed)+int(power.Int64()) > 19 {
		return 0, errors.New("invalid federation integer")
	}
	return strconv.ParseInt(match[1]+trimmed+strings.Repeat("0", int(power.Int64())), 10, 64)
}

func decodeFederationVersions(raw []byte) (int, error) {
	canonical, err := federationJSON(raw)
	if err != nil {
		return 0, err
	}
	fields, err := registrationObject(canonical)
	if err != nil {
		return 0, err
	}
	var legacy []json.RawMessage
	if !registrationPresent(fields["supported"]) || json.Unmarshal(fields["supported"], &legacy) != nil {
		return 0, errors.New("invalid version information")
	}
	for _, v := range legacy {
		if _, err := federationInteger(v); err != nil {
			return 0, err
		}
	}
	versions := []json.RawMessage{json.RawMessage("0")}
	if registrationPresent(fields["supported_versions"]) {
		if json.Unmarshal(fields["supported_versions"], &versions) != nil {
			return 0, errors.New("invalid version information")
		}
	}
	best := -1
	for _, v := range versions {
		n, err := federationInteger(v)
		if err != nil {
			return 0, err
		}
		if n >= 0 && n <= 4 && int(n) > best {
			best = int(n)
		}
	}
	if best < 0 {
		return 0, prekeyFederationFailure{code: 533, label: "federation-version-error", message: "Version negotiation failed: the remote backend is too old"}
	}
	return best, nil
}

func (s prekeyFederationClient) request(ctx context.Context, domain, path, requestID string, version *int, body []byte) ([]byte, http.Header, error) {
	if s.endpoint == nil {
		return nil, nil, prekeyFederationFailure{code: 400, label: "federation-not-enabled", message: "no federator configured"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+s.endpoint.Address()+"/rpc/"+domain+"/brig/"+path, bytes.NewReader(body))
	if err != nil {
		return nil, nil, prekeyFederationFailure{code: 500, label: "federation-local-error", message: "Invalid local federator request"}
	}
	req.Header.Set("Wire-Origin-Domain", s.origin)
	req.Header.Set("Wire-Origin-Request-Id", requestID)
	// Native streaming negotiation bypasses the versioned RunClient instance.
	if version != nil {
		req.Header.Set("Wire-Federation-Version", strconv.Itoa(*version))
		req.Header.Set("Accept", "application/json")
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := s.client.Do(req)
	if err != nil {
		return nil, nil, prekeyFederationFailure{code: 500, label: "federation-not-available", message: "Local federator connection failed"}
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 16<<20+1))
	if err != nil || len(raw) > 16<<20 {
		return nil, nil, prekeyFederationFailure{code: 533, label: "federation-invalid-body", message: "Could not parse body"}
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return raw, response.Header, nil
	}
	if response.StatusCode == 403 {
		return nil, nil, prekeyFederationFailure{code: 500, label: "federation-local-error", message: "Local federator failure: " + strings.ToValidUTF8(string(raw), "�")}
	}
	failure := prekeyFederationFailure{code: response.StatusCode, label: "unknown-federation-error", message: strings.ToValidUTF8(string(raw), "�"), data: map[string]string{"type": "federation", "domain": domain, "path": "/federation/" + path}}
	canonical, decodeErr := federationJSON(raw)
	if decodeErr == nil {
		fields, err := registrationObject(canonical)
		if err == nil {
			code, codeErr := federationInteger(fields["code"])
			label, labelErr := registrationText(fields, "label", true)
			message, messageErr := registrationText(fields, "message", true)
			if codeErr == nil && code >= 100 && code <= 999 && labelErr == nil && messageErr == nil {
				failure.code, failure.label, failure.message = int(code), *label, *message
				_ = json.Unmarshal(fields["inner"], &failure.inner)
			}
		}
	}
	return nil, nil, failure
}

func (s prekeyFederationClient) call(ctx context.Context, domain, path, requestID string, input any) (json.RawMessage, error) {
	raw, _, err := s.request(ctx, domain, "api-version", requestID, nil, []byte("[]"))
	if err != nil {
		return nil, err
	}
	version, err := decodeFederationVersions(raw)
	if err != nil {
		var failure prekeyFederationFailure
		if errors.As(err, &failure) {
			return nil, err
		}
		return nil, prekeyFederationFailure{code: 533, label: "federation-version-error", message: "Remote federator returned invalid version information"}
	}
	body, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	raw, headers, err := s.request(ctx, domain, path, requestID, &version, body)
	if err != nil {
		return nil, err
	}
	header := headers.Get("Content-Type")
	media, _, err := mime.ParseMediaType(header)
	if err != nil && header != "" {
		return nil, prekeyFederationFailure{code: 533, label: "federation-invalid-content-type", message: "Content-type: " + header}
	}
	if header == "" {
		media = "application/octet-stream"
	}
	if media != "application/json" {
		return nil, prekeyFederationFailure{code: 533, label: "federation-unsupported-content-type", message: "Content-type: " + header + ", Media-Type: " + media}
	}
	canonical, err := federationJSON(raw)
	if err != nil {
		return nil, prekeyFederationFailure{code: 533, label: "federation-invalid-body", message: "Could not parse body"}
	}
	return canonical, nil
}
