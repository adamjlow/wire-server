// SPDX-License-Identifier: AGPL-3.0-only
package federator

import (
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"golang.org/x/net/dns/dnsmessage"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBoundaryValidation(t *testing.T) {
	for _, v := range []string{"example.com", "localhost", "a-b.example", "0.example"} {
		if !validDomain(v) {
			t.Errorf("valid domain rejected %q", v)
		}
	}
	for _, v := range []string{"127.0.0.1", "a.1com", "-a.example", "a-.example", "a..example", "example.com.", "evil/host.example", "a@b.example", "a.éxample", "single", strings.Repeat("a", 64) + ".example"} {
		if validDomain(v) {
			t.Errorf("unsafe domain accepted %q", v)
		}
	}
	for _, v := range []string{"", "foo/bar", "..", "query?x", "foo.bar", "%2f"} {
		if validRPC(v) {
			t.Errorf("unsafe RPC accepted %q", v)
		}
	}
	if !validRPC("get-user_1") {
		t.Fatal("valid RPC rejected")
	}
	if _, err := parseCertificate("not-a-certificate"); err == nil {
		t.Fatal("invalid certificate accepted")
	}
	if originIP("unknown, [2001:db8::1]:443, 192.0.2.1") != "2001:db8::1" {
		t.Fatal("incorrect origin IP")
	}
	if originIP("malicious.example:80") != "" {
		t.Fatal("hostname trusted as IP")
	}
}
func TestCertificateIdentityBoundary(t *testing.T) {
	c := &x509.Certificate{DNSNames: []string{"*.example.com"}, Subject: pkix.Name{CommonName: "untrusted.example"}}
	if !certificateMatches(c, "one.example.com") || certificateMatches(c, "a.b.example.com") || certificateMatches(c, "untrusted.example") {
		t.Fatal("SAN/wildcard mismatch boundary")
	}
	c.DNSNames = nil
	c.Subject.CommonName = "legacy.example.com"
	if !certificateMatches(c, "legacy.example.com") || certificateMatches(c, "other.example.com") {
		t.Fatal("reference CN fallback")
	}
}
func TestDNSWireOrderAndFailures(t *testing.T) {
	packet, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer packet.Close()
	done := make(chan struct{})
	defer close(done)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, addr, err := packet.ReadFrom(buf)
			if err != nil {
				return
			}
			var msg dnsmessage.Message
			if msg.Unpack(buf[:n]) != nil {
				continue
			}
			msg.Response = true
			msg.RecursionAvailable = true
			name := msg.Questions[0].Name.String()
			msg.Answers = nil
			if strings.Contains(name, "failed.") {
				msg.RCode = dnsmessage.RCodeServerFailure
			} else if strings.Contains(name, "missing.") {
				msg.RCode = dnsmessage.RCodeNameError
			} else {
				for _, value := range []struct {
					host     string
					priority uint16
				}{{"first.example.com.", 30}, {"second.example.com.", 1}} {
					host, _ := dnsmessage.NewName(value.host)
					msg.Answers = append(msg.Answers, dnsmessage.Resource{Header: dnsmessage.ResourceHeader{Name: msg.Questions[0].Name, Type: dnsmessage.TypeSRV, Class: dnsmessage.ClassINET, TTL: 1}, Body: &dnsmessage.SRVResource{Priority: value.priority, Port: 443, Target: host}})
				}
			}
			data, _ := msg.Pack()
			_, _ = packet.WriteTo(data, addr)
		}
	}()
	targets, err := dnsTargets(t.Context(), packet.LocalAddr().String(), "order.example.com")
	if err != nil || len(targets) != 2 || targets[0].Host != "first.example.com." {
		t.Fatalf("wire order lost %v %v", targets, err)
	}
	if targets, err = dnsTargets(t.Context(), packet.LocalAddr().String(), "missing.example.com"); err != nil || len(targets) != 0 {
		t.Fatal("missing SRV must not become transport failure")
	}
	if _, err = dnsTargets(t.Context(), packet.LocalAddr().String(), "failed.example.com"); err == nil {
		t.Fatal("DNS failure accepted")
	}
}
func TestVerifiedTransportAndH2Requirement(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "safe") }))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()
	timeout := int64(500000)
	s := &Service{config: Config{Settings: Settings{TCPConnectionTimeout: &timeout}}}
	transport := s.transport(&tls.Config{MinVersion: tls.VersionTLS12})
	defer transport.CloseIdleConnections()
	client := http.Client{Transport: transport}
	if resp, err := client.Get(server.URL); err == nil {
		resp.Body.Close()
		t.Fatal("untrusted TLS accepted")
	}
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	transport = s.transport(&tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12})
	defer transport.CloseIdleConnections()
	client.Transport = transport
	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.ProtoMajor != 2 {
		t.Fatal("verified HTTP2 unavailable")
	}
	transport = s.transport(&tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12, ServerName: "wrong.invalid"})
	defer transport.CloseIdleConnections()
	client.Transport = transport
	if resp, err = client.Get(server.URL); err == nil {
		resp.Body.Close()
		t.Fatal("wrong hostname accepted")
	}

}
func TestPerRequestPolicyAndH2C(t *testing.T) {
	policy := "allowAll"
	calls := 0
	brig := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"strategy":%q,"remotes":[],"update_interval":10}`, policy)
	}))
	defer brig.Close()
	host, port, _ := net.SplitHostPort(strings.TrimPrefix(brig.URL, "http://"))
	var p int
	fmt.Sscan(port, &p)
	s := &Service{config: Config{Brig: Endpoint{host, uint16(p)}}, local: brig.Client()}
	for _, strategy := range []string{"allowAll", "allowNone", "allowAll"} {
		policy = strategy
		w := httptest.NewRecorder()
		got := s.allow(t.Context(), "example.com", "synthetic", w)
		if got != (strategy == "allowAll") {
			t.Fatal("policy cached or denied incorrectly")
		}
	}
	if calls != 3 {
		t.Fatal("policy not fetched per request")
	}
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, r.ProtoMajor) }))
	server.Config.Protocols = protocols
	server.Start()
	defer server.Close()
	clientProtocols := new(http.Protocols)
	clientProtocols.SetUnencryptedHTTP2(true)
	tr := &http.Transport{Protocols: clientProtocols}
	defer tr.CloseIdleConnections()
	client := http.Client{Transport: tr}
	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "2" {
		t.Fatal("Haskell HTTP2 peer transport unavailable")
	}
}
func TestMetricLabelsAndStreamingWriter(t *testing.T) {
	m := newMetrics()
	m.observe("/rpc/example.com/brig/test", "POST", 533, time.Millisecond)
	w := httptest.NewRecorder()
	m.serve(w)
	if !strings.Contains(w.Body.String(), `handler="/rpc/:domain/:component/:rpc",method="POST",status_code="533"`) {
		t.Fatal("lost existing operational labels")
	}
	writer := &observedWriter{ResponseWriter: httptest.NewRecorder()}
	stream(writer, strings.NewReader("binary"))
	if writer.status != 200 {
		t.Fatal("streaming wrapper dropped status")
	}
}

func TestMalformedPolicyFailsClosed(t *testing.T) {
	for _, body := range []string{`{"strategy":"allowAll"}`, `{"strategy":"unknown","remotes":[],"update_interval":10}`, `{"strategy":"allowAll","remotes":[],"update_interval":10} trailing`, `{"strategy":"allowAll","remotes":[{"domain":"example.com","search_policy":"full_search","restriction":{"tag":"allow_all"}}],"update_interval":10}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
		host, port, _ := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
		var p int
		fmt.Sscan(port, &p)
		s := &Service{config: Config{Brig: Endpoint{host, uint16(p)}}, local: server.Client()}
		w := httptest.NewRecorder()
		if s.allow(t.Context(), "example.com", "synthetic", w) || w.Code != 500 {
			t.Fatalf("malformed policy permitted: %s", body)
		}
		server.Close()
	}
}
