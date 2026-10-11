// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/binary"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
	"go.yaml.in/yaml/v3"
)

func syntheticMLSCountPackage(before, after uint64, source byte) []byte {
	// Deliberately empty keys/signatures: native counting checks syntax/lifetime,
	// leaving authentication to upload validation.
	value := []byte{0, 1, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, source}
	if source == 1 {
		value = binary.BigEndian.AppendUint64(value, before)
		value = binary.BigEndian.AppendUint64(value, after)
	}
	if source == 3 {
		value = append(value, 0)
	}
	return append(value, 0, 0, 0, 0)
}
func TestMLSCountDecodeLifetimeAndDeleteBeforeLookup(t *testing.T) {
	now := time.Unix(100, 0)
	for _, tc := range []struct {
		before, after uint64
		source        byte
		usable        bool
	}{
		{100, 101, 1, true}, {101, 102, 1, false}, {0, 100, 1, false}, {0, 101, 2, false}, {0, 101, 3, false},
	} {
		value, ok := decodeMLSPackageLifetime(syntheticMLSCountPackage(tc.before, tc.after, tc.source))
		if !ok || value.usable(now, nil) != tc.usable {
			t.Fatalf("lifetime %+v decoded=%v value=%+v", tc, ok, value)
		}
	}
	valid := syntheticMLSCountPackage(0, 101, 1)
	for n := range len(valid) {
		if _, ok := decodeMLSPackageLifetime(valid[:n]); ok {
			t.Fatalf("accepted truncation at %d", n)
		}
	}
	if _, ok := decodeMLSPackageLifetime(append(append([]byte{}, valid...), 0)); ok {
		t.Fatal("accepted trailing data")
	}
	// Native VarInt decoding permits nonminimal encodings.
	wide := append(append([]byte{}, valid[:4]...), 0x40, 0)
	wide = append(wide, valid[5:]...)
	if _, ok := decodeMLSPackageLifetime(wide); !ok {
		t.Fatal("rejected native nonminimal VarInt")
	}
	user := gocql.TimeUUID()
	trace := []string{}
	failure := false
	s := mlsClientReads{now: func() time.Time { return now }, packages: func(context.Context, gocql.UUID, string, uint16) ([]mlsPackageRow, error) {
		trace = append(trace, "packages")
		return []mlsPackageRow{{ref: []byte{1}, data: valid}, {ref: []byte{2}, data: syntheticMLSCountPackage(0, 100, 1)}, {ref: []byte{3}, data: []byte{0}}}, nil
	}, remove: func(_ context.Context, _ gocql.UUID, _ string, _ uint16, refs [][]byte) error {
		trace = append(trace, "delete")
		if len(refs) != 1 || refs[0][0] != 2 {
			t.Fatal("malformed retained / expired deleted policy changed")
		}
		if failure {
			return errors.New("synthetic deletion failure")
		}
		return nil
	}, get: func(context.Context, gocql.UUID, string) (*storedClient, error) {
		trace = append(trace, "client")
		return &storedClient{PublicKeys: map[string][]byte{"ed25519": {1, 2}}}, nil
	}}
	info, err := s.single(context.Background(), user, "a", 1)
	if err != nil || !info.MLS || info.SignatureKey == nil || *info.SignatureKey != "AQI=" || strings.Join(trace, ",") != "packages,delete,client" {
		t.Fatalf("info=%+v trace=%v err=%v", info, trace, err)
	}
	failure = true
	trace = nil
	_, err = s.single(context.Background(), user, "a", 1)
	if err == nil || strings.Join(trace, ",") != "packages,delete" {
		t.Fatal("deletion failure did not precede metadata lookup")
	}
}

func TestMLSMaximumLifetimeExactSeconds(t *testing.T) {
	var maximum mlsLifetimeMaximum
	if err := yaml.Unmarshal([]byte("0.25"), &maximum); err != nil {
		t.Fatal(err)
	}
	p := mlsPackageLifetime{source: 1, before: 100, after: 101}
	if !p.usable(time.Unix(100, 750_000_000), &maximum) || p.usable(time.Unix(100, 749_999_999), &maximum) {
		t.Fatal("maximum lifetime boundary changed")
	}
	for _, raw := range []string{".nan", ".inf", "\"123\""} {
		if yaml.Unmarshal([]byte(raw), &maximum) == nil {
			t.Fatalf("accepted invalid lifetime %s", raw)
		}
	}
}

func TestMLSFederationVersionsAndMissingClient(t *testing.T) {
	user := gocql.TimeUUID()
	suiteCalls := []uint16{}
	s := mlsClientReads{now: time.Now, ids: func(context.Context, gocql.UUID) ([]string, error) { return []string{"a"}, nil }, packages: func(_ context.Context, _ gocql.UUID, _ string, suite uint16) ([]mlsPackageRow, error) {
		suiteCalls = append(suiteCalls, suite)
		return nil, nil
	}, remove: func(context.Context, gocql.UUID, string, uint16, [][]byte) error { return nil }, get: func(context.Context, gocql.UUID, string) (*storedClient, error) { return nil, nil }}
	h := newHandler(Config{}, &brigReadHandlers{mlsClients: &s})
	for _, tc := range []struct {
		version, path, body string
		status              int
	}{
		{"0", "/federation/get-mls-clients", `{"user_id":"` + user.String() + `","signature_scheme":"ecdsa_secp384r1_sha384"}`, 200},
		{"1", "/federation/get-mls-clients", `{"user_id":"` + user.String() + `","cipher_suite":7}`, 200},
		{"2", "/federation/get-mls-client", `{}`, 404},
		{"3", "/federation/get-mls-client", `{"user_id":"` + user.String() + `","client_id":"f","cipher_suite":2}`, 200},
		{"4", "/federation/get-mls-client", `{"user_id":"` + user.String() + `","client_id":"f","cipher_suite":3}`, 400},
	} {
		r := httptest.NewRequest("POST", tc.path, strings.NewReader(tc.body))
		r.Header.Set("X-Wire-API-Version", tc.version)
		r.Header.Set("Wire-Origin-Domain", "remote.example")
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("v%s %s: %d %s", tc.version, tc.path, w.Code, w.Body.String())
		}
		if tc.version == "3" && strings.TrimSpace(w.Body.String()) != `{"id":"f","mls":false}` {
			t.Fatalf("unknown client contract: %s", w.Body.String())
		}
	}
	if len(suiteCalls) != 3 || suiteCalls[0] != 1 || suiteCalls[1] != 7 || suiteCalls[2] != 2 {
		t.Fatalf("legacy suite or invalid request effects changed: %v", suiteCalls)
	}
}

func TestPublicMLSCountFeatureAndVersions(t *testing.T) {
	user := gocql.TimeUUID()
	calls := 0
	s := mlsClientReads{enabled: true, now: time.Now, packages: func(context.Context, gocql.UUID, string, uint16) ([]mlsPackageRow, error) { calls++; return nil, nil }, remove: func(context.Context, gocql.UUID, string, uint16, [][]byte) error { return nil }}
	for _, tc := range []struct {
		version, query string
		enabled        bool
		status         int
		handled        bool
	}{
		{"4", "?ciphersuite=0x1", true, 0, false}, {"7", "", true, 200, true}, {"19", "?ciphersuite=0x1", true, 200, true}, {"19", "", true, 400, true}, {"19", "?ciphersuite=0x3", true, 400, true}, {"19", "?ciphersuite=0x1", false, 400, true},
	} {
		s.enabled = tc.enabled
		r := httptest.NewRequest("GET", "/mls/key-packages/self/f/count"+tc.query, nil)
		r.Header.Set("X-Wire-API-Version", tc.version)
		r.Header.Set("Z-User", user.String())
		w := httptest.NewRecorder()
		before := calls
		handled := s.handle(w, r, "/mls/key-packages/self/f/count")
		if handled != tc.handled || handled && w.Code != tc.status {
			t.Fatalf("v%s: handled=%v %d %s", tc.version, handled, w.Code, w.Body.String())
		}
		if tc.status != 200 && calls != before {
			t.Fatal("invalid/disabled count accessed storage")
		}
	}
	if calls != 2 {
		t.Fatalf("count calls=%d", calls)
	}
}

func TestMLSDeleteAdmissionAndNativeStorageEffects(t *testing.T) {
	user := gocql.TimeUUID()
	calls := 0
	s := mlsClientReads{enabled: true, remove: func(_ context.Context, got gocql.UUID, client string, suite uint16, refs [][]byte) error {
		calls++
		if got != user || client != "a" || suite != 1 || len(refs) != 3 || len(refs[0]) != 1 || refs[0][0] != 1 || refs[1][0] != 1 || len(refs[2]) != 0 {
			t.Fatal("delete changed refs or queried client metadata")
		}
		return nil
	}}
	body := `{"key_packages":["AQ==","AQ==",""]}`
	for _, tc := range []struct {
		version, query, body string
		enabled              bool
		status               int
	}{
		{"4", "?ciphersuite=1", body, true, 404},
		{"7", "", body, true, 201},
		{"19", "?ciphersuite=1", body, true, 201},
		{"19", "", body, true, 400},
		{"19", "?ciphersuite=3", body, true, 400},
		{"19", "?ciphersuite=1", body, false, 400},
		{"19", "?ciphersuite=1", `{"key_packages":[]}`, true, 400},
		{"19", "?ciphersuite=1", `{"key_packages":[null]}`, true, 400},
		{"19", "?ciphersuite=1", `{"key_packages":["!invalid"]}`, true, 400},
	} {
		s.enabled = tc.enabled
		req := httptest.NewRequest("DELETE", "/mls/key-packages/self/0a"+tc.query, strings.NewReader(tc.body))
		req.Header.Set("X-Wire-API-Version", tc.version)
		req.Header.Set("Z-User", user.String())
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		if !s.handle(response, req, "/mls/key-packages/self/0a") {
			response.WriteHeader(404)
		}
		if response.Code != tc.status {
			t.Fatalf("version%s status%d want%d", tc.version, response.Code, tc.status)
		}
		if tc.status == 201 && (response.Body.Len() != 0 || response.Header().Get("Content-Type") != "") {
			t.Fatal("native delete success is empty201")
		}
	}
	if calls != 2 {
		t.Fatal("invalid input reached storage")
	}
	// A bounded reference array is required, but each reference is an arbitrary
	// blob: neither reference length nor duplicate elimination is native policy.
	oversized := `{"key_packages":[` + strings.Repeat(`"AQ==",`, 1000) + `"AQ=="]}`
	if _, err := decodeMLSDeleteReferences([]byte(oversized)); err == nil {
		t.Fatal("accepted more than1000 refs")
	}
}
