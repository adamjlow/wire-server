// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

// Independent synthetic signer. Real mls-test-cli packages are covered by the
// unchanged compiled Haskell tests, not by this local generator.
func signedMLSFixture(t *testing.T, suite uint16, identity string, protocol uint16, missingCapability, badLeaf bool) ([]byte, []byte) {
	t.Helper()
	vector := func(data []byte) []byte {
		var prefix []byte
		if len(data) < 64 {
			prefix = []byte{byte(len(data))}
		} else {
			prefix = []byte{byte(len(data)>>8) | 0x40, byte(len(data))}
		}
		return append(prefix, data...)
	}
	var key []byte
	var sign func([]byte) []byte
	if suite == 1 {
		public, private, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		key = public
		sign = func(data []byte) []byte { return ed25519.Sign(private, data) }
	} else {
		curve := map[uint16]elliptic.Curve{2: elliptic.P256(), 7: elliptic.P384(), 5: elliptic.P521()}[suite]
		private, err := ecdsa.GenerateKey(curve, rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		key = elliptic.Marshal(curve, private.X, private.Y)
		sign = func(data []byte) []byte {
			var hash []byte
			switch suite {
			case 7:
				h := sha512.Sum384(data)
				hash = h[:]
			case 5:
				h := sha512.Sum512(data)
				hash = h[:]
			default:
				h := sha256.Sum256(data)
				hash = h[:]
			}
			sig, err := ecdsa.SignASN1(rand.Reader, private, hash)
			if err != nil {
				t.Fatal(err)
			}
			return sig
		}
	}
	labelled := func(label string, data []byte) []byte {
		return append(vector([]byte("MLS 1.0 "+label)), vector(data)...)
	}
	leaf := append([]byte{0}, vector(key)...)
	leaf = append(leaf, 0, 1)
	leaf = append(leaf, vector([]byte(identity))...)
	leaf = append(leaf, 2, 0, 1, 2, byte(suite>>8), byte(suite), 0, 0)
	if missingCapability {
		leaf = append(leaf, 0)
	} else {
		leaf = append(leaf, 2, 0, 1)
	}
	leaf = append(leaf, 1)
	leaf = binary.BigEndian.AppendUint64(leaf, 0)
	leaf = binary.BigEndian.AppendUint64(leaf, uint64(time.Now().Unix()+3600))
	leaf = append(leaf, 0)
	leafSignature := sign(labelled("LeafNodeTBS", leaf))
	if badLeaf {
		leafSignature[0] ^= 1
	}
	tbs := binary.BigEndian.AppendUint16(nil, protocol)
	tbs = binary.BigEndian.AppendUint16(tbs, suite)
	tbs = append(tbs, 0)
	tbs = append(tbs, leaf...)
	tbs = append(tbs, vector(leafSignature)...)
	tbs = append(tbs, 0)
	return append(tbs, vector(sign(labelled("KeyPackageTBS", tbs)))...), key
}

func TestMLSSignedUploadValidationAndAtomicAdmission(t *testing.T) {
	user := gocql.TimeUUID()
	identity := user.String() + ":a@example.com"
	for _, suite := range []uint16{1, 2, 7, 5} {
		raw, key := signedMLSFixture(t, suite, identity, 1, false, false)
		p, err := decodeUploadedMLSPackage(raw)
		if err != nil || p.validate(identity) != nil {
			t.Fatalf("suite%d decode/validation: %v", suite, err)
		}
		for n := range len(raw) {
			if _, err := decodeUploadedMLSPackage(raw[:n]); err == nil {
				t.Fatalf("accepted truncation%d", n)
			}
		}
		if _, err := decodeUploadedMLSPackage(append(raw, 0)); err == nil {
			t.Fatal("accepted trailing data")
		}
		writes := 0
		present := true
		stored := key
		s := mlsClientReads{domain: "example.com", now: time.Now, key: func(context.Context, gocql.UUID, string, string) ([]byte, bool, error) { return stored, present, nil }, insert: func(_ context.Context, _ gocql.UUID, _ string, values []validatedMLSPackage) error {
			writes++
			if len(values) != 1 || len(values[0].ref) != len(mlsHash(suite, nil)) || string(values[0].data) != string(raw) {
				t.Fatal("raw upload not retained")
			}
			return nil
		}}
		if err := s.upload(context.Background(), user, "a", []uploadedMLSPackage{p}); err != nil {
			t.Fatal(err)
		}
		present = false
		if err := s.upload(context.Background(), user, "a", []uploadedMLSPackage{p}); err == nil {
			t.Fatal("missing stored key accepted")
		}
		present = true
		stored = []byte{0}
		if err := s.upload(context.Background(), user, "a", []uploadedMLSPackage{p}); err == nil {
			t.Fatal("mismatched stored key accepted")
		}
		stored = key
		invalid := p
		invalid.signature = append([]byte{}, p.signature...)
		invalid.signature[0] ^= 1
		if err := s.upload(context.Background(), user, "a", []uploadedMLSPackage{p, invalid}); err == nil {
			t.Fatal("partially validated batch accepted")
		}
		if writes != 1 {
			t.Fatal("rejected upload wrote package data")
		}
	}
	for _, tc := range []struct {
		protocol         uint16
		capability, leaf bool
		label, message   string
	}{
		{2, false, false, "mls-protocol-error", "Unsupported protocol version"},
		{1, true, false, "mls-protocol-error", "Missing BasicCredential capability"},
		{1, false, true, "mls-invalid-leaf-node-signature", "Invalid leaf node signature in key package"},
	} {
		raw, _ := signedMLSFixture(t, 1, identity, tc.protocol, tc.capability, tc.leaf)
		p, err := decodeUploadedMLSPackage(raw)
		if err != nil {
			t.Fatal(err)
		}
		var failure mlsValidationFailure
		if !errors.As(p.validate(identity), &failure) || failure.label != tc.label || failure.message != tc.message {
			t.Fatalf("wrong validation failure: %+v", failure)
		}
	}
}

func TestMLSReplaceDeletesBeforeUploadValidation(t *testing.T) {
	user := gocql.TimeUUID()
	raw, _ := signedMLSFixture(t, 1, user.String()+":a@example.com", 1, false, false)
	trace := []string{}
	s := mlsClientReads{domain: "example.com", enabled: true, now: time.Now, removeSuite: func(context.Context, gocql.UUID, string, uint16) error { trace = append(trace, "delete"); return nil }, key: func(context.Context, gocql.UUID, string, string) ([]byte, bool, error) {
		trace = append(trace, "key")
		return nil, false, nil
	}, insert: func(context.Context, gocql.UUID, string, []validatedMLSPackage) error {
		t.Fatal("rejected replacement inserted")
		return nil
	}}
	body, _ := json.Marshal(map[string]any{"key_packages": []string{base64.StdEncoding.EncodeToString(raw)}})
	r := httptest.NewRequest("PUT", "/mls/key-packages/self/a?ciphersuites=1", strings.NewReader(string(body)))
	r.Header.Set("Z-User", user.String())
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Wire-API-Version", "19")
	w := httptest.NewRecorder()
	if !s.handle(w, r, "/mls/key-packages/self/a") || w.Code != 400 || strings.Join(trace, ",") != "delete,key" {
		t.Fatalf("native replacement order: status%d trace%v", w.Code, trace)
	}
}
