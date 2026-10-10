// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"strings"
	"testing"
)

type countedFormBody struct {
	*strings.Reader
	read int
}

func (b *countedFormBody) Read(p []byte) (int, error) {
	n, err := b.Reader.Read(p)
	b.read += n
	return n, err
}
func (b *countedFormBody) Close() error { return nil }

func TestSAMLFormRejectionBeforeAuthentication(t *testing.T) {
	for _, kind := range []string{"raw-limit", "invalid-utf8", "unsupported-type", "multipart-name", "multipart-files", "semicolon"} {
		t.Run(kind, func(t *testing.T) {
			contentType := "application/x-www-form-urlencoded"
			payload := []byte("SAMLResponse=dG9rZW4=")
			wantStatus, wantLabel := 500, "server-error"
			switch kind {
			case "raw-limit":
				payload = []byte(strings.Repeat("x", samlFormLimit+1))
			case "invalid-utf8":
				payload = []byte("SAMLResponse=%FF")
			case "unsupported-type":
				contentType = "application/json"
				wantStatus, wantLabel = 400, ""
			case "multipart-name", "multipart-files":
				var data bytes.Buffer
				writer := multipart.NewWriter(&data)
				if kind == "multipart-name" {
					if err := writer.WriteField(strings.Repeat("x", 33), "x"); err != nil {
						t.Fatal(err)
					}
				} else {
					for index := 0; index < 11; index++ {
						file, err := writer.CreateFormFile("file", "synthetic.txt")
						if err != nil {
							t.Fatal(err)
						}
						_, _ = io.WriteString(file, "synthetic")
					}
				}
				if err := writer.WriteField("SAMLResponse", "dG9rZW4="); err != nil {
					t.Fatal(err)
				}
				if err := writer.Close(); err != nil {
					t.Fatal(err)
				}
				contentType, payload = writer.FormDataContentType(), data.Bytes()
			case "semicolon":
				payload = []byte("unused=a;b&SAMLResponse=dG9rZW4=")
				wantStatus, wantLabel = 400, "wire:sso:error:bad-response-xml"
			}
			peers, idp, _, _ := loginFixture(t)
			store := &finalizeBoundary{loginBoundary: peers, judgeMemory: &judgeMemory{}, idp: idp}
			request := httptest.NewRequest("POST", "/sso/finalize-login", bytes.NewReader(payload))
			request.Header.Set("Content-Type", contentType)
			response := httptest.NewRecorder()
			response.Header().Set("Cache-Control", "no-cache, no-store")
			response.Header().Set("Pragma", "no-cache")
			config := Config{SAML: SAMLConfig{DomainConfig: DomainConfig{SSOURI: "https://synthetic.example/sso"}}}
			if !serveSAMLFinalize(response, request, config, store, peers) || response.Code != wantStatus {
				t.Fatal("form rejection status", response.Code, response.Body.String())
			}
			if len(peers.events) != 0 || len(store.judgeMemory.events) != 0 || response.Header().Get("Set-Cookie") != "" {
				t.Fatal("invalid form reached authentication or storage effects")
			}
			if wantStatus == 500 {
				var errorBody struct{ Label, Message string }
				if json.Unmarshal(response.Body.Bytes(), &errorBody) != nil || errorBody.Label != wantLabel || errorBody.Message != "Internal Server Error" || response.Header().Get("Content-Type") != "application/json" || response.Header().Get("Cache-Control") != "" || response.Header().Get("Pragma") != "" {
					t.Fatal("native outer exception projection", response.Body.String())
				}
			} else if kind == "semicolon" && !strings.Contains(response.Body.String(), wantLabel) {
				t.Fatal("semicolon rejected before native SAML XML parsing")
			}
		})
	}
}

func TestSAMLRawFormReadBound(t *testing.T) {
	body := &countedFormBody{Reader: strings.NewReader(strings.Repeat("x", 1<<20))}
	request := httptest.NewRequest("POST", "/sso/finalize-login", nil)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Body = body
	if _, err := parseSAMLForm(request); err != errSAMLFormLimit || body.read != samlFormLimit+1 {
		t.Fatal("oversized form read past rejection boundary", body.read, err)
	}
}
