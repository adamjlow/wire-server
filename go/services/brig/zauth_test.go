// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"
)

const syntheticUser = "00000000-0000-0000-0000-000000000001"

func TestCanonicalSignedTokenFields(t *testing.T) {
	client := "ab"
	for _, tc := range []struct{ kind, body string }{
		{"a", "u=" + syntheticUser + ".i=ab.c=42"},
		{"la", "u=" + syntheticUser + ".i=ab.c=42"},
		{"u", "u=" + syntheticUser + ".r=beef.i=ab"},
		{"lu", "u=" + syntheticUser + ".r=beef.i=ab"},
		{"p", "p=" + syntheticUser},
		{"b", "p=" + syntheticUser + ".b=" + syntheticUser + ".c=" + syntheticUser},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			z := ZToken{Version: 1, Key: 2, Kind: tc.kind, Expires: *big.NewInt(100), Session: true, User: syntheticUser, Provider: syntheticUser, Bot: syntheticUser, Conversation: syntheticUser, Connection: 42, Random: 0xbeef, Client: &client}
			got, err := z.canonical()
			want := "v=1.k=2.d=100.t=" + tc.kind + ".l=s." + tc.body
			if err != nil || string(got) != want {
				t.Fatalf("canonical token: %q, %v", got, err)
			}
		})
	}
}

func TestZAuthCanonicalVerificationAndSecurityBoundaries(t *testing.T) {
	// RFC8032 test1 seed, synthetic public test material, not production key.
	seed, _ := hex.DecodeString("9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60")
	key := ed25519.NewKeyFromSeed(seed)
	keys := []ed25519.PublicKey{key.Public().(ed25519.PublicKey)}
	z := ZToken{Version: 1, Key: 1, Kind: "u", Expires: *big.NewInt(100), User: syntheticUser, Random: 0xab}
	if err := z.Sign(key); err != nil {
		t.Fatal(err)
	}
	encoded, err := z.Encode()
	if err != nil {
		t.Fatal(err)
	}
	signature := strings.Split(encoded, ".")[0]
	// Native validates the canonical parsed data. Unknown properties and later
	// duplicate fields cannot override the first parsed authenticated principal.
	alternate := signature + ".u=" + strings.ToUpper(syntheticUser) + ".r=00AB.t=u.k=01.v=01.d=0100.extra=ignored.u=00000000-0000-0000-0000-000000000002"
	parsed, err := ParseZToken(alternate, "u")
	if err != nil {
		t.Fatal(err)
	}
	if err = parsed.Verify(keys, time.Unix(100, 999)); err != nil {
		t.Fatalf("expiry equality/canonical verification: %v", err)
	}
	if err = parsed.Verify(keys, time.Unix(101, 0)); !errors.Is(err, ErrExpiredToken) {
		t.Fatalf("expiry: %v", err)
	}
	if _, err = ParseZToken(encoded, "lu"); !errors.Is(err, ErrInvalidToken) {
		t.Fatal("accepted user token as legal hold")
	}
	parsed.User = "00000000-0000-0000-0000-000000000002"
	if err = parsed.Verify(keys, time.Unix(99, 0)); !errors.Is(err, ErrFalsifiedToken) {
		t.Fatal("accepted changed principal")
	}
	parsed = z
	parsed.Key = 0
	if err = parsed.Verify(keys, time.Unix(99, 0)); !errors.Is(err, ErrInvalidToken) {
		t.Fatal("accepted key zero")
	}
	parsed = z
	parsed.Key = 2
	if err = parsed.Verify(keys, time.Unix(99, 0)); !errors.Is(err, ErrInvalidToken) {
		t.Fatal("accepted absent rotation key")
	}
	parsed = z
	parsed.Signature = []byte{1}
	if err = parsed.Verify(keys, time.Unix(99, 0)); !errors.Is(err, ErrFalsifiedToken) {
		t.Fatal("accepted truncated signature")
	}
	z.Expires.SetInt64(-1)
	if err = z.Sign(key); err != nil {
		t.Fatal(err)
	}
	if err = z.Verify(keys, time.Unix(1<<50, 0)); err != nil {
		t.Fatalf("native never expires: %v", err)
	}
}

func TestZAuthUnknownTagAndEmptyClient(t *testing.T) {
	prefix := base64.URLEncoding.EncodeToString(make([]byte, 64))
	parsed, err := ParseZToken(prefix+".v=1.k=1.d=-1.t=a.l=unknown.u="+syntheticUser+".i=.c=0", "a")
	if err != nil || parsed.Session || parsed.Client == nil || *parsed.Client != "" {
		t.Fatalf("native optional fields: %v", err)
	}
	data, err := parsed.canonical()
	if err != nil || !strings.Contains(string(data), ".l=.u=") || !strings.HasSuffix(string(data), ".i=.c=0") {
		t.Fatalf("canonical optional fields: %s, %v", data, err)
	}
}
