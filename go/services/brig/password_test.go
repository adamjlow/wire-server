// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

func TestScryptPublishedVectorAndNativeFixedLengthVerification(t *testing.T) {
	// RFC 7914's first vector, also exercised by the official Go package.
	key, err := hex.DecodeString("77d6576238657b203b19ca42c18a0497f16b4844e3074ae8dfdffa3fede21442fcd0069ded0948f8326a753a0fc81f17e8d3e0fb2e0d3628cf35e20c38d18906")
	if err != nil {
		t.Fatal(err)
	}
	encoded := "4|1|1||" + base64.StdEncoding.EncodeToString(key)
	ok, update, err := verifyPasswordHash(encoded, "", true)
	if err != nil || !ok || !update {
		t.Fatal("published vector or upgrade status mismatch")
	}
	ok, _, err = verifyPasswordHash(encoded, "wrong", false)
	if err != nil || ok {
		t.Fatal("wrong password accepted")
	}
	short := "4|1|1||" + base64.StdEncoding.EncodeToString(key[:16])
	ok, _, err = verifyPasswordHash(short, "", false)
	if err != nil || ok {
		t.Fatal("verification incorrectly derived stored key length")
	}
}

func TestNativePasswordGenerationFormatsAndVerificationStatus(t *testing.T) {
	plain := "Synthetic-password-\u00e9-2026!"
	for _, argon := range []bool{false, true} {
		encoded, err := newPasswordHash(plain, argon, 128, 1, 1)
		if err != nil {
			t.Fatal(err)
		}
		h, err := parsePasswordHash(encoded)
		if err != nil {
			t.Fatal(err)
		}
		if argon {
			parts := strings.Split(encoded, "$")
			if len(h.salt) != 16 || len(h.key) != 16 || h.algorithm != "argon2id" || h.version != 19 || strings.ContainsAny(parts[len(parts)-2]+parts[len(parts)-1], "=") {
				t.Fatal("Argon2 native generation format changed")
			}
		} else if len(h.salt) != 32 || len(h.key) != 64 || h.rounds != 14 || h.blockSize != 8 || h.parallelism != 1 {
			t.Fatal("Scrypt native defaults changed")
		}
		for _, argonPolicy := range []bool{false, true} {
			ok, update, err := verifyPasswordHash(encoded, plain, argonPolicy)
			if err != nil || !ok || update != (!argon && argonPolicy) {
				t.Fatal("policy changed valid credentials or upgrade status")
			}
		}
		second, err := newPasswordHash(plain, argon, 128, 1, 1)
		if err != nil || second == encoded {
			t.Fatal("random salt was reused")
		}
		if strings.Contains(fmt.Sprintf("%v %#v", h, h), plain) || fmt.Sprintf("%v %#v", h, h) != "<Password> <Password>" {
			t.Fatal("password formatting is not redacted")
		}
	}
}

func TestArgonHistoricalVariantsAndVersions(t *testing.T) {
	salt := base64.RawStdEncoding.EncodeToString([]byte("synthetic-salt16"))
	key := base64.RawStdEncoding.EncodeToString(make([]byte, 16))
	for _, variant := range []string{"argon2i", "argon2d", "argon2id"} {
		for _, version := range []string{"", "v=16$", "v=19$"} {
			encoded := "$" + variant + "$" + version + "p=1,m=128,t=1,m=256$" + salt + "$" + key
			h, err := parsePasswordHash(encoded)
			if err != nil || h.memory != 256 {
				t.Fatal("native field order/last duplicate or historical version parsing changed")
			}
			_, err = h.derive("synthetic")
			if err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestArgonFullLaneCountAndOmittedVersion(t *testing.T) {
	h := passwordHash{algorithm: "argon2id", version: 16, memory: 2048, iterations: 1, parallelism: 256, salt: []byte("synthetic-salt16")}
	key, err := h.derive("Synthetic-password")
	if err != nil || len(key) != 16 {
		t.Fatal("uint32 lane count was narrowed to uint8")
	}
	h.key = key
	encoded := h.encode()
	omitted := strings.Replace(encoded, "v=16$", "", 1)
	for _, value := range []string{encoded, omitted} {
		ok, update, err := verifyPasswordHash(value, "Synthetic-password", true)
		if err != nil || !ok || update {
			t.Fatal("legacy version/lane count authentication mismatch")
		}
	}
	short := h
	short.key = h.key[:8]
	if ok, _, err := verifyPasswordHash(short.encode(), "Synthetic-password", false); err != nil || ok {
		t.Fatal("Argon2 verification used stored output length instead of native16")
	}
}

func TestPasswordMalformedParametersDoNotAllocateOrDisclose(t *testing.T) {
	for _, encoded := range []string{"synthetic-private-value", "4294967296|8|1||", "64|8|1||", "14|0|1||", "14|8|0||", "$argon2id$v=19$m=128,t=0,p=1$c3ludGhldGljLXNhbHQ$", "$argon2id$v=19$m=128,t=1,p=1,x=2$c3ludGhldGljLXNhbHQ$"} {
		_, _, err := verifyPasswordHash(encoded, "synthetic-private-value", true)
		if err == nil || strings.Contains(err.Error(), "synthetic-private-value") {
			t.Fatal("invalid password parameters accepted or disclosed")
		}
	}
}
