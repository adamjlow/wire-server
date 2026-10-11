// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAccessRenewalHTTPClientCanonicalizationAndCookieSecurity(t *testing.T) {
	for _, insecure := range []bool{false, true} {
		m, cookies, user := testSessionManager()
		created := time.Unix(1700000000, 0)
		old, err := m.newCookie(context.Background(), user, nil, 1, nil, false, false, created)
		if err != nil {
			t.Fatal(err)
		}
		value, _ := old.token.Encode()
		s := &accessRenewalHTTP{service: accessRenewalService{
			tokens:  accessTokenService{lookup: &memoryCookieLookup{store: cookies}, sessions: m, keys: []ed25519.PublicKey{m.issuer.key.Public().(ed25519.PublicKey)}, now: func() time.Time { return created.Add(time.Second) }},
			clients: &renewalClientLookup{client: &storedClient{ID: "1"}},
		}, cookieInsecure: insecure}
		r := httptest.NewRequest(http.MethodPost, "/access?client_id=00001", nil)
		r.Header.Set("Cookie", "zuid="+value)
		w := httptest.NewRecorder()
		newHandler(Config{}, &brigReadHandlers{renewal: s}).ServeHTTP(w, r)
		if w.Code != 200 || w.Header().Get("Content-Type") != "application/json;charset=utf-8" {
			t.Fatal("renewal HTTP failed", w.Code, w.Body.String())
		}
		var body struct {
			User  string
			Token string `json:"access_token"`
			Type  string `json:"token_type"`
			TTL   int64  `json:"expires_in"`
		}
		if json.Unmarshal(w.Body.Bytes(), &body) != nil || body.User != user.String() || body.Type != "Bearer" || body.TTL != 30 {
			t.Fatal("native access wire fields")
		}
		token, err := ParseZToken(body.Token, "a")
		if err != nil || token.Client == nil || *token.Client != "1" {
			t.Fatal("query client ID not canonicalized", err)
		}
		issued := w.Result().Cookies()
		if len(issued) != 1 || issued[0].Name != "zuid" || issued[0].Path != "/access" || !issued[0].HttpOnly || issued[0].Secure == insecure || issued[0].Expires.IsZero() {
			t.Fatal("native persistent cookie attributes", issued)
		}
		userToken, err := ParseZToken(issued[0].Value, "u")
		if err != nil || userToken.Client == nil || *userToken.Client != "1" {
			t.Fatal("replacement cookie client binding", err)
		}
		// Ordinary renewal without a replacement must not send Set-Cookie.
		r = httptest.NewRequest(http.MethodPost, "/access", nil)
		r.Header.Set("Cookie", "zuid="+issued[0].Value)
		w = httptest.NewRecorder()
		s.handle(w, r)
		if w.Code != 200 || w.Header().Get("Set-Cookie") != "" {
			t.Fatal("young cookie unexpectedly replaced")
		}
	}
}

func TestAccessRenewalHTTPRejectsBadClientBeforeCredentials(t *testing.T) {
	w := httptest.NewRecorder()
	accessRenewalHTTP{}.handle(w, httptest.NewRequest(http.MethodPost, "/access?client_id=not-hex", nil))
	if w.Code != 400 || !strings.Contains(w.Body.String(), "client_id") {
		t.Fatal("query parser precedence")
	}
}
