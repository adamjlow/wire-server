// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// http-client0.7.19 snapshots proxy variables when creating its manager.
// Exact lowercase names win; otherwise the last case-folded name wins. Its
// no_proxy entries are literal domain suffixes, without Go's localhost,
// wildcard, IP range or port rules.
func internalHTTPProxy(environment []string) (func(*http.Request) (*url.URL, error), error) {
	exact, folded := map[string]string{}, map[string]string{}
	for _, entry := range environment {
		name, value, ok := strings.Cut(entry, "=")
		if ok {
			exact[name], folded[strings.ToLower(name)] = value, value
		}
	}
	lookup := func(name string) string {
		if value, ok := exact[name]; ok {
			return value
		}
		return folded[name]
	}
	proxies := map[string]*url.URL{}
	for _, scheme := range []string{"http", "https"} {
		value := lookup(scheme + "_proxy")
		if value == "" {
			continue
		}
		if !strings.HasPrefix(value, "http://") {
			value = "http://" + value
		}
		proxy, err := url.Parse(value)
		if err != nil || proxy.Scheme != "http" || proxy.Hostname() == "" || strings.HasSuffix(proxy.Host, ":") ||
			(proxy.EscapedPath() != "" && proxy.EscapedPath() != "/") ||
			proxy.RawQuery != "" || proxy.ForceQuery || proxy.Fragment != "" || strings.Contains(value, "#") {
			// Never include proxy credentials or the environment value in errors.
			return nil, errors.New("invalid internal HTTP proxy environment")
		}
		proxies[scheme] = proxy
	}
	prefix := func(host string) string {
		if strings.HasPrefix(host, ".") {
			return host
		}
		return "." + host
	}
	var suffixes []string
	for _, suffix := range strings.Split(strings.ToLower(lookup("no_proxy")), ",") {
		if suffix == "" {
			continue
		}
		suffixes = append(suffixes, prefix(strings.TrimLeft(suffix, " ")))
	}
	return func(request *http.Request) (*url.URL, error) {
		host := prefix(strings.ToLower(request.URL.Hostname()))
		for _, suffix := range suffixes {
			if strings.HasSuffix(host, suffix) {
				return nil, nil
			}
		}
		return proxies[request.URL.Scheme], nil
	}, nil
}
