// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"math"
	"strings"
	"testing"
)

func TestActivationKeyUsesFullPaddedHashAndNativeEmailAliases(t *testing.T) {
	a, err := activationEmailKey("Synthetic+alias@example.com")
	if err != nil {
		t.Fatal(err)
	}
	b, err := activationEmailKey("synthetic@example.com")
	if err != nil || a != b || len(a) != 44 || !strings.HasSuffix(a, "=") {
		t.Fatal("activation key changed", err)
	}
	verification, err := emailVerificationKey("synthetic@example.com")
	if err != nil || len(verification) != 20 || verification == a {
		t.Fatal("activation and verification key contracts merged", err)
	}
	trusted, _ := activationEmailKey("Synthetic+alias@wire.com")
	plain, _ := activationEmailKey("synthetic@wire.com")
	if trusted == plain {
		t.Fatal("trusted domain alias discarded")
	}
	if strings.Contains((activationRecord{email: "synthetic-private@example.com", code: "123456"}).String(), "123456") {
		t.Fatal("activation record exposed code")
	}
}

func TestNativeCassandraTTLHalfEvenAndHistoricalInt32Wrap(t *testing.T) {
	for _, example := range []struct {
		seconds float64
		want    int32
	}{{30.5, 30}, {31.5, 32}, {-1.5, -2}, {0, 0}, {2147483648, -2147483648}} {
		got, err := cassandraTTL(example.seconds)
		if err != nil || got != example.want {
			t.Fatal(example.seconds, got, err)
		}
	}
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := cassandraTTL(value); err == nil {
			t.Fatal("nonfinite TTL accepted")
		}
	}
}
