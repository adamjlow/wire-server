package cargohold

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"encoding/base64"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCloudFrontSignaturePolicyAndExpiry(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer := cloudFrontSigner{key: key, domain: "cdn.example.com", keyPairID: "synthetic-key-id"}
	signed, err := signer.signedURL("v3/persistent/11111111-1111-4111-8111-111111111111", time.Unix(1700000000, 0))
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(signed)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	expiry, err := strconv.ParseInt(q.Get("Expires"), 10, 64)
	if err != nil || expiry != 1700000300 || q.Get("Key-Pair-Id") != "synthetic-key-id" || u.Host != "cdn.example.com" {
		t.Fatalf("unexpected signed URL fields: %s", signed)
	}
	signature, err := base64.StdEncoding.DecodeString(strings.NewReplacer("-", "+", "_", "=", "~", "/").Replace(q.Get("Signature")))
	if err != nil {
		t.Fatal(err)
	}
	// Independently constructed policy, not the production policy helper.
	digest := sha1.Sum([]byte(`{"Statement":[{"Resource":"https://cdn.example.com/v3/persistent/11111111-1111-4111-8111-111111111111","Condition":{"DateLessThan":{"AWS:EpochTime":1700000300}}}]}`))
	if err = rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA1, digest[:], signature); err != nil {
		t.Fatalf("signature did not verify: %v", err)
	}
	digest[0] ^= 1
	if rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA1, digest[:], signature) == nil {
		t.Fatal("modified policy verified")
	}
}
