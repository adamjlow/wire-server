package cargohold

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type cloudFrontSigner struct {
	key               *rsa.PrivateKey
	domain, keyPairID string
}

func newCloudFrontSigner(settings Config) (*cloudFrontSigner, error) {
	c := settings.AWS.CloudFront
	if c == nil {
		return nil, nil
	}
	data, err := os.ReadFile(c.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("cannot read CloudFront private key")
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("invalid CloudFront private key")
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		parsed, e := x509.ParsePKCS8PrivateKey(block.Bytes)
		if e != nil {
			return nil, fmt.Errorf("invalid CloudFront private key")
		}
		var ok bool
		key, ok = parsed.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("CloudFront requires RSA private key")
		}
	}
	if err = key.Validate(); err != nil {
		return nil, fmt.Errorf("invalid CloudFront private key")
	}
	return &cloudFrontSigner{key: key, domain: c.Domain, keyPairID: c.KeyPairID}, nil
}

func cloudFrontPolicy(resource string, expires int64) []byte {
	b, _ := json.Marshal(resource)
	return []byte(fmt.Sprintf(`{"Statement":[{"Resource":%s,"Condition":{"DateLessThan":{"AWS:EpochTime":%d}}}]}`, b, expires))
}

func (c *cloudFrontSigner) signedURL(key string, now time.Time) (string, error) {
	expires := int64(math.RoundToEven(float64(now.Unix())+float64(now.Nanosecond())/1e9)) + 300
	resource := "https://" + c.domain + "/" + key
	// RSA-SHA1 is the existing CloudFront canned-policy wire protocol.
	digest := sha1.Sum(cloudFrontPolicy(resource, expires))
	signature, err := rsa.SignPKCS1v15(rand.Reader, c.key, crypto.SHA1, digest[:])
	if err != nil {
		return "", err
	}
	encoded := strings.NewReplacer("+", "-", "=", "_", "/", "~").Replace(base64.StdEncoding.EncodeToString(signature))
	return resource + "?Expires=" + strconv.FormatInt(expires, 10) + "&Signature=" + encoded + "&Key-Pair-Id=" + url.QueryEscape(c.keyPairID), nil
}
