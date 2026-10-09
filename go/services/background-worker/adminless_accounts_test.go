// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func nativeAccountGolden(t *testing.T, n int) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "..", "libs", "wire-api", "test", "golden", fmt.Sprintf("testObject_User_user_%d.json", n)))
	if err != nil {
		t.Fatal(err)
	}
	return body
}
func TestAdminlessAccountsReuseFiveOriginalUserGoldens(t *testing.T) {
	for n := 1; n <= 5; n++ {
		body := nativeAccountGolden(t, n)
		user, err := decodeAdminlessAccount(body)
		if err != nil || user.ID == "" || user.Name == "" {
			t.Fatalf("unchanged native User golden%d: %v", n, err)
		}
		decoded := decodeAdminlessAccounts(append(append([]byte("["), body...), ']'))
		if len(decoded) != 1 || decoded[0] != user {
			t.Fatal("native golden chunk changed")
		}
	}
}
func TestAdminlessAccountsFullRowValidationAndNativeDefaults(t *testing.T) {
	base := nativeAccountGolden(t, 4)
	for field, bad := range map[string]string{
		"qualified_id": `{"id":"bad","domain":"wire.com"}`, "type": `"unknown"`, "name": `""`, "status": `"inactive"`, "accent_id": `"1"`, "locale": `"invalid"`,
		"email": `"bad email"`, "email_unvalidated": `42`, "sso_id": `{"tenant":"x"}`, "text_status": `""`, "picture": `[null]`, "assets": `[{"type":"image","key":"bad"}]`, "service": `{"id":"bad"}`, "handle": `"A"`, "expires_at": `"invalid"`, "team": `"bad"`, "managed_by": `"other"`, "supported_protocols": `["other"]`, "deleted": `1`, "searchable": `"true"`,
	} {
		var object map[string]json.RawMessage
		if json.Unmarshal(base, &object) != nil {
			t.Fatal("fixture JSON")
		}
		object[field] = json.RawMessage(bad)
		body, _ := json.Marshal(object)
		if _, err := decodeAdminlessAccount(body); err == nil {
			t.Fatalf("unused invalid field %s accepted", field)
		}
		// One bad user discards every user in the successful chunk, preserving source.
		chunk := append(append(append([]byte("["), base...), ','), body...)
		chunk = append(chunk, ']')
		if len(decodeAdminlessAccounts(chunk)) != 0 {
			t.Fatalf("invalid %s user was selectively filtered", field)
		}
	}
	var object map[string]json.RawMessage
	_ = json.Unmarshal(base, &object)
	for _, field := range []string{"assets", "picture", "service", "team", "handle", "expires_at", "managed_by", "supported_protocols", "email", "sso_id", "searchable", "deleted", "text_status"} {
		object[field] = json.RawMessage("null")
	}
	object["id"] = json.RawMessage(`{"malformed":"deprecated"}`)
	object["locale"] = json.RawMessage(`"en-NOT-A-COUNTRY"`)
	object["unknown_future_field"] = json.RawMessage(`{"arbitrary":true}`)
	body, _ := json.Marshal(object)
	user, err := decodeAdminlessAccount(body)
	if err != nil || user.Service {
		t.Fatal("nullable/default/deprecated ID or native locale suffix semantics changed")
	}
	for _, colour := range []string{"-2147483648", "2147483647", "1.000e0", "0e999999999"} {
		if !validAdminlessAccountColour(json.RawMessage(colour)) {
			t.Fatalf("valid colour %s", colour)
		}
	}
	for _, colour := range []string{"-2147483649", "2147483648", "1.1", "1e999999999", `"1"`} {
		if validAdminlessAccountColour(json.RawMessage(colour)) {
			t.Fatalf("bad colour %s", colour)
		}
	}
	for _, raw := range [][]byte{[]byte("null"), []byte("{}"), []byte("[null]"), []byte("invalid"), append([]byte("[\""), 255)} {
		if len(decodeAdminlessAccounts(raw)) != 0 {
			t.Fatal("invalid body not empty")
		}
	}
}
func TestAdminlessAccountNativeTimestampGrammar(t *testing.T) {
	for _, text := range []string{"2026-10-09T12:34:56.123456789123999Z", "2026-10-09 12:34Z", "2026-10-09T12:34+14", "2026-10-09T12:34-1200", "2026-10-09T12:34+01:30", "2026-10-09T12:34:60.5Z", "+2026-10-09T12:34Z", "-0400-02-29T12:34Z", "123456789012345600-02-29T12:34Z"} {
		if !validAdminlessAccountTime(text) {
			t.Fatalf("valid native time %q", text)
		}
	}
	for _, text := range []string{"2026-10-09 12:34 Z", "2026-02-29T12:34Z", "1900-02-29T12:34Z", "2026-10-09t12:34Z", "2026-10-09T12:34", "2026-10-09T24:00Z", "2026-10-09T12:60Z", "2026-10-09T12:34:61Z", "2026-10-09T12:34:56.Z", "2026-10-09T12:34+14:01", "2026-10-09T12:34-12:01", "2026-10-09T12:34+01:60"} {
		if validAdminlessAccountTime(text) {
			t.Fatalf("invalid native time %q", text)
		}
	}
}
func brigAccountPeer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, Endpoint) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	host, port, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	p, _ := strconv.Atoi(port)
	return server, Endpoint{Host: host, Port: p}
}
func TestAdminlessBrigSourceChunkingHeaderAndMalformedFallback(t *testing.T) {
	body := nativeAccountGolden(t, 1)
	calls := 0
	server, endpoint := brigAccountPeer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" || r.URL.Path != "/i/users" || r.Header.Get("Request-Id") != "synthetic-request" {
			t.Error("native request contract changed")
		}
		ids := strings.Split(r.URL.Query().Get("ids"), ",")
		want := 100
		if calls == 3 {
			want = 1
		}
		if len(ids) != want {
			t.Errorf("chunk%d count=%d", calls, len(ids))
		}
		if calls == 2 {
			_, _ = w.Write([]byte(`[{}]`))
			return
		}
		_, _ = w.Write(append(append([]byte("["), body...), ']'))
	})
	b := adminlessBrigAccounts{Endpoint: endpoint, Client: server.Client(), RequestID: "synthetic-request"}
	ids := make([]string, 201)
	for i := range ids {
		ids[i] = "00000000-0000-0000-0000-000000000001"
	}
	users, err := b.Users(t.Context(), ids)
	if err != nil || calls != 3 || len(users) != 2 {
		t.Fatalf("chunks calls=%d users=%d err=%v", calls, len(users), err)
	}
	if _, err := b.Users(t.Context(), nil); err != nil || calls != 3 {
		t.Fatal("empty lookup issued request")
	}
	if _, err := b.Users(t.Context(), []string{"bad&ids=synthetic"}); err == nil || calls != 3 {
		t.Fatal("invalid ID reached peer")
	}
}
func TestAdminlessBrigNativeRetriesAndSafeDiagnostics(t *testing.T) {
	for _, status := range []int{500, 400} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			calls := 0
			server, endpoint := brigAccountPeer(t, func(w http.ResponseWriter, _ *http.Request) {
				calls++
				w.WriteHeader(status)
				_, _ = w.Write([]byte("synthetic sensitive diagnostic"))
			})
			b := adminlessBrigAccounts{Endpoint: endpoint, Client: server.Client()}
			_, err := b.Users(t.Context(), []string{"00000000-0000-0000-0000-000000000001"})
			want := 1
			if status == 500 {
				want = 4
			}
			if err == nil || calls != want || bytes.Contains([]byte(err.Error()), []byte("sensitive")) {
				t.Fatalf("retry calls=%d error=%v", calls, err)
			}
		})
	}
}

func TestAdminlessAccountJSONSurrogatesMatchNativeFailure(t *testing.T) {
	for _, text := range []string{`["\ud800"]`, `["\udc00"]`, `["\ud800\u0041"]`} {
		if validAdminlessAccountJSONText([]byte(text)) {
			t.Fatal("unpaired surrogate became replacement text")
		}
	}
	for _, text := range []string{`["\ud800\udc00"]`, `["\\ud800"]`, `["valid é"]`} {
		if !validAdminlessAccountJSONText([]byte(text)) {
			t.Fatal("valid text rejected")
		}
	}
}
