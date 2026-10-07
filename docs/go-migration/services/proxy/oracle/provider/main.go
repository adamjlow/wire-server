// Synthetic reference fixture only; never a production service.
package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"
)

const limit = 1024 * 1024

type profile struct {
	Statuses []int       `json:"statuses"`
	Headers  http.Header `json:"headers"`
	Body     []byte      `json:"body"`
	Fault    string      `json:"fault,omitempty"`
	DelayMS  int         `json:"delay_ms,omitempty"`
}

type observation struct {
	Method  string      `json:"method"`
	Target  string      `json:"target"`
	Host    string      `json:"host"`
	Proto   string      `json:"protocol"`
	Headers http.Header `json:"headers"`
	Body    []byte      `json:"body"`
}

type fixture struct {
	mu      sync.Mutex
	profile profile
	seen    []observation
}

type clientRequest struct {
	Method  string      `json:"method"`
	Target  string      `json:"target"`
	Headers [][2]string `json:"headers"`
	Body    []byte      `json:"body_base64"`
}

func clientEndpoint(role string) (string, error) {
	switch role {
	case "reference":
		return "http://reference:8080", nil
	case "candidate":
		return "http://candidate:8080", nil
	case "control":
		return "http://127.0.0.1:8081", nil
	default:
		return "", fmt.Errorf("invalid fixture client role")
	}
}

func runClient(role string, input io.Reader, output io.Writer) error {
	endpoint, err := clientEndpoint(role)
	if err != nil {
		return err
	}
	data, err := io.ReadAll(io.LimitReader(input, 2*limit+1))
	if err != nil || len(data) > 2*limit {
		return fmt.Errorf("fixture client input exceeds limit")
	}
	var in clientRequest
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&in) != nil || d.Decode(new(any)) != io.EOF || len(in.Body) > limit ||
		len(in.Headers) > 100 || len(in.Target) > 32768 || !strings.HasPrefix(in.Target, "/") {
		return fmt.Errorf("invalid fixture client request")
	}
	r, err := http.NewRequest(in.Method, endpoint+in.Target, bytes.NewReader(in.Body))
	if err != nil {
		return fmt.Errorf("invalid fixture client request")
	}
	for _, pair := range in.Headers {
		if len(pair[0]) > 256 || len(pair[1]) > 4096 || strings.ContainsAny(pair[0]+pair[1], "\r\n\x00") {
			return fmt.Errorf("invalid fixture client headers")
		}
		r.Header.Add(pair[0], pair[1])
	}
	client := &http.Client{Timeout: 12 * time.Second,
		Transport:     &http.Transport{DisableCompression: true, Proxy: nil},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(r)
	if err != nil {
		return fmt.Errorf("fixture client transport failed")
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 8*limit+1))
	if err != nil || len(payload) > 8*limit {
		return fmt.Errorf("fixture response exceeds limit")
	}
	return encodeResponse(output, response, payload)
}

// net/http consumes framing headers while parsing. Retain those fields alongside
// headers/trailers so a comparator cannot hide invalid framing as normalization.
func encodeResponse(output io.Writer, response *http.Response, payload []byte) error {
	return json.NewEncoder(output).Encode(struct {
		Status           int         `json:"status"`
		Headers          http.Header `json:"headers"`
		Body             []byte      `json:"body_base64"`
		Protocol         string      `json:"protocol"`
		ContentLength    int64       `json:"content_length"`
		TransferEncoding []string    `json:"transfer_encoding"`
		Close            bool        `json:"close"`
		Uncompressed     bool        `json:"uncompressed"`
		Trailers         http.Header `json:"trailers"`
	}{response.StatusCode, response.Header, append([]byte{}, payload...),
		response.Proto, response.ContentLength, append([]string{}, response.TransferEncoding...),
		response.Close, response.Uncompressed, response.Trailer})
}

func validProfile(p profile) bool {
	if len(p.Statuses) < 1 || len(p.Statuses) > 10 || len(p.Body) > limit || len(p.Headers) > 20 {
		return false
	}
	if (p.Fault != "" && p.Fault != "close" && p.Fault != "incomplete-headers") || p.DelayMS < 0 || p.DelayMS > 6000 {
		return false
	}
	for _, s := range p.Statuses {
		if s < 200 || s > 599 {
			return false
		}
	}
	for name, values := range p.Headers {
		if name != "Content-Type" && name != "Content-Encoding" && name != "Location" && name != "X-Provider" && name != "Retry-After" {
			return false
		}
		if len(values) > 10 {
			return false
		}
		for _, value := range values {
			if len(value) > 4096 || strings.ContainsAny(value, "\r\n\x00") {
				return false
			}
		}
	}
	return true
}

func (f *fixture) provider(w http.ResponseWriter, r *http.Request) {
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if err != nil {
		http.Error(w, "fixture request too large", http.StatusRequestEntityTooLarge)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.seen) >= 100 {
		http.Error(w, "fixture observation limit", 503)
		return
	}
	f.seen = append(f.seen, observation{Method: r.Method, Target: r.RequestURI,
		Host: r.Host, Proto: r.Proto, Headers: r.Header.Clone(), Body: b})
	if f.profile.Fault != "" {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		if f.profile.Fault == "incomplete-headers" {
			_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\nX-Synthetic: incomplete")
		}
		_ = conn.Close()
		return
	}
	if f.profile.DelayMS > 0 {
		timer := time.NewTimer(time.Duration(f.profile.DelayMS) * time.Millisecond)
		defer timer.Stop()
		select {
		case <-r.Context().Done():
			return
		case <-timer.C:
		}
	}
	index := min(len(f.seen)-1, len(f.profile.Statuses)-1)
	for name, values := range f.profile.Headers {
		for _, value := range values {
			w.Header().Add(name, value)
		}
	}
	w.WriteHeader(f.profile.Statuses[index])
	_, _ = w.Write(f.profile.Body)
}

func (f *fixture) control(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == "POST" && r.URL.Path == "/reset":
		var p profile
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit*2))
		d.DisallowUnknownFields()
		if d.Decode(&p) != nil || d.Decode(new(any)) != io.EOF || !validProfile(p) {
			http.Error(w, "invalid synthetic profile", 400)
			return
		}
		f.profile, f.seen = p, nil
		_, _ = w.Write([]byte("{}"))
	case r.Method == "GET" && r.URL.Path == "/observations":
		// Empty effects must serialize as [] rather than null.
		seen := append([]observation{}, f.seen...)
		_ = json.NewEncoder(w).Encode(seen)
	default:
		http.NotFound(w, r)
	}
}

func certificateFiles(dir string) error {
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	now := time.Now()
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "synthetic proxy fixture CA"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(24 * time.Hour), IsCA: true,
		BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		return err
	}
	server := &x509.Certificate{SerialNumber: big.NewInt(2), DNSNames: []string{"provider"},
		NotBefore: ca.NotBefore, NotAfter: ca.NotAfter, KeyUsage: x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	serverDER, err := x509.CreateCertificate(rand.Reader, server, ca, &key.PublicKey, caKey)
	if err != nil {
		return err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return err
	}
	for name, block := range map[string]*pem.Block{
		"ca.pem":         {Type: "CERTIFICATE", Bytes: caDER},
		"server.pem":     {Type: "CERTIFICATE", Bytes: serverDER},
		"server-key.pem": {Type: "PRIVATE KEY", Bytes: keyDER},
	} {
		f, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0444)
		if err != nil {
			return err
		}
		err = pem.Encode(f, block)
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func main() {
	initDir := flag.String("init", "", "generate ephemeral synthetic CA/server certificate files")
	clientRole := flag.String("client", "", "request reference or local control from inside the isolated network")
	buildInfo := flag.Bool("build-info", false, "emit fixture compiler/module/source build identity")
	flag.Parse()
	if *buildInfo {
		info, ok := debug.ReadBuildInfo()
		if !ok || json.NewEncoder(os.Stdout).Encode(info) != nil {
			os.Exit(1)
		}
		return
	}
	if *clientRole != "" {
		if runClient(*clientRole, os.Stdin, os.Stdout) != nil {
			fmt.Fprintln(os.Stderr, "synthetic fixture client failed")
			os.Exit(1)
		}
		return
	}
	if *initDir != "" {
		if err := certificateFiles(*initDir); err != nil {
			fmt.Fprintln(os.Stderr, "synthetic certificate initialization failed")
			os.Exit(1)
		}
		return
	}
	f := &fixture{profile: profile{Statuses: []int{200}, Headers: http.Header{"Content-Type": {"application/json"}}, Body: []byte(`{"provider":"synthetic"}`)}}
	server := func(address string, handler http.HandlerFunc) *http.Server {
		return &http.Server{Addr: address, Handler: handler, ReadHeaderTimeout: 3 * time.Second,
			ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 5 * time.Second, MaxHeaderBytes: 32768}
	}
	go func() {
		if server(":8081", f.control).ListenAndServe() != nil {
			os.Exit(1)
		}
	}()
	if server(":8443", f.provider).ListenAndServeTLS("/fixture/server.pem", "/fixture/server-key.pem") != nil {
		os.Exit(1)
	}
}
