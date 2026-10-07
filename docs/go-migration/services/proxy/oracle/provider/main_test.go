package main

import (
	"bufio"
	"bytes"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestControlCanObserveRequestBeforeDelayedResponseCompletes(t *testing.T) {
	f := &fixture{profile: profile{Statuses: []int{200}, DelayMS: 150}}
	done := make(chan struct{})
	go func() { f.provider(httptest.NewRecorder(), httptest.NewRequest("GET", "/inflight", nil)); close(done) }()
	deadline := time.Now().Add(time.Second)
	for {
		w := httptest.NewRecorder()
		f.control(w, httptest.NewRequest("GET", "/observations", nil))
		var observations []observation
		_ = json.Unmarshal(w.Body.Bytes(), &observations)
		if len(observations) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("in-flight request was not visible")
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case <-done:
		t.Fatal("control blocked until response completion; shutdown evidence would be invalid")
	default:
	}
	<-done
}

func TestResponseRetainsFramingHeadersTrailersAndBytes(t *testing.T) {
	r, err := http.ReadResponse(bufio.NewReader(strings.NewReader("HTTP/1.1 200 OK\r\nContent-Type: a\r\nContent-Type: b\r\nTransfer-Encoding: chunked\r\nTrailer: X-Final\r\n\r\n2\r\nhi\r\n0\r\nX-Final: retained\r\n\r\n")), nil)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := encodeResponse(&output, r, body); err != nil {
		t.Fatal(err)
	}
	var observed struct {
		Protocol         string      `json:"protocol"`
		ContentLength    int64       `json:"content_length"`
		TransferEncoding []string    `json:"transfer_encoding"`
		Headers          http.Header `json:"headers"`
		Trailers         http.Header `json:"trailers"`
		Body             []byte      `json:"body_base64"`
	}
	if json.Unmarshal(output.Bytes(), &observed) != nil || observed.Protocol != "HTTP/1.1" || observed.ContentLength != -1 || !reflect.DeepEqual(observed.TransferEncoding, []string{"chunked"}) || !reflect.DeepEqual(observed.Headers.Values("Content-Type"), []string{"a", "b"}) || observed.Trailers.Get("X-Final") != "retained" || string(observed.Body) != "hi" {
		t.Fatal("response observations lost framing or application values")
	}
}

func TestClientRejectsExternalRolesAndInvalidInputBeforeNetwork(t *testing.T) {
	for _, role := range []string{"http://production.invalid", "provider", ""} {
		if _, err := clientEndpoint(role); err == nil {
			t.Fatal("external role accepted")
		}
	}
	for _, body := range []string{
		`{"method":"GET","target":"https://production.invalid"}`,
		`{"method":"GET","target":"/","unknown":true}`,
		`{"method":"GET","target":"/","headers":[["X-Input","a\r\nb"]]}`,
		strings.Repeat("x", 2*limit+1),
	} {
		if runClient("reference", strings.NewReader(body), new(bytes.Buffer)) == nil {
			t.Fatal("invalid client input accepted")
		}
	}
}

func TestProfilesRejectUnsafeInput(t *testing.T) {
	for _, p := range []profile{
		{Statuses: []int{199}}, {Statuses: []int{600}}, {},
		{Statuses: []int{200}, Headers: http.Header{"Location": {"x\r\ny"}}},
		{Statuses: []int{200}, Headers: http.Header{"Set-Cookie": {"x"}}},
		{Statuses: []int{200}, Body: make([]byte, limit+1)},
		{Statuses: []int{200}, Fault: "uncontrolled"},
		{Statuses: []int{200}, DelayMS: -1},
		{Statuses: []int{200}, DelayMS: 6001},
	} {
		if validProfile(p) {
			t.Fatal("unsafe profile accepted")
		}
	}
}

func TestProviderCapturesDuplicatesBytesAndAttempts(t *testing.T) {
	f := &fixture{profile: profile{Statuses: []int{502, 200}, Body: []byte("synthetic")}}
	for _, status := range []int{502, 200, 200} {
		r := httptest.NewRequest("POST", "/path?a=1&a=2", strings.NewReader("payload"))
		r.Header.Add("Accept", "a")
		r.Header.Add("Accept", "b")
		w := httptest.NewRecorder()
		f.provider(w, r)
		if w.Code != status {
			t.Fatalf("status %d, want %d", w.Code, status)
		}
	}
	if len(f.seen) != 3 || f.seen[0].Target != "/path?a=1&a=2" || string(f.seen[0].Body) != "payload" || len(f.seen[0].Headers["Accept"]) != 2 {
		t.Fatal("effects lost")
	}
}

func TestControlRejectsTrailingAndUnknownFields(t *testing.T) {
	f := &fixture{}
	for _, body := range []string{`{"statuses":[200],"headers":{},"body":""} {}`, `{"statuses":[200],"unknown":true}`} {
		w := httptest.NewRecorder()
		f.control(w, httptest.NewRequest("POST", "/reset", strings.NewReader(body)))
		if w.Code != 400 {
			t.Fatal("invalid control accepted")
		}
	}
	w := httptest.NewRecorder()
	f.control(w, httptest.NewRequest("GET", "/observations", nil))
	var observations []observation
	if json.Unmarshal(w.Body.Bytes(), &observations) != nil || observations == nil || len(observations) != 0 {
		t.Fatal("empty effects not []")
	}
}

func TestSyntheticCertificateVerifiesHostAndCannotOverwrite(t *testing.T) {
	dir := t.TempDir()
	if err := certificateFiles(dir); err != nil {
		t.Fatal(err)
	}
	read := func(name string) *x509.Certificate {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		block, _ := pem.Decode(b)
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			t.Fatal(err)
		}
		return cert
	}
	pool := x509.NewCertPool()
	pool.AddCert(read("ca.pem"))
	cert := read("server.pem")
	if _, err := cert.Verify(x509.VerifyOptions{Roots: pool, DNSName: "provider"}); err != nil {
		t.Fatal(err)
	}
	if _, err := cert.Verify(x509.VerifyOptions{Roots: pool, DNSName: "other-provider"}); err == nil {
		t.Fatal("wrong host accepted")
	}
	if certificateFiles(dir) == nil {
		t.Fatal("existing certificate overwritten")
	}
}
