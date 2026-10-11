// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type mlsValidationFailure struct{ label, message string }

func (e mlsValidationFailure) Error() string { return "MLS validation failed" }
func mlsProtocolFailure(message string) error {
	return mlsValidationFailure{"mls-protocol-error", message}
}

type uploadedMLSPackage struct {
	raw, tbs, leafTBS, signatureKey, signature, leafSignature []byte
	protocol, suite, credential                               uint16
	identity                                                  []byte
	certificates                                              [][]byte
	credentialCapabilities                                    []uint16
	lifetime                                                  mlsPackageLifetime
}

// Raw TBS slices preserve native RawMLS bytes, including nonminimal VarInts.
func decodeUploadedMLSPackage(raw []byte) (uploadedMLSPackage, error) {
	c := mlsCursor{raw: raw}
	p := uploadedMLSPackage{raw: raw}
	p.protocol = uint16(c.number(2))
	p.suite = uint16(c.number(2))
	c.bytes()
	leafStart := len(raw) - len(c.raw)
	c.bytes()
	p.signatureKey = c.bytes()
	p.credential = uint16(c.number(2))
	switch p.credential {
	case 1:
		p.identity = c.bytes()
	case 2:
		v := mlsCursor{raw: c.bytes()}
		for len(v.raw) > 0 && !v.bad {
			p.certificates = append(p.certificates, v.bytes())
		}
		c.bad = c.bad || v.bad
	default:
		c.bad = true
	}
	for i := range 5 {
		v := c.bytes()
		if len(v)%2 != 0 {
			c.bad = true
		}
		if i == 4 {
			for n := 0; n+1 < len(v); n += 2 {
				p.credentialCapabilities = append(p.credentialCapabilities, binary.BigEndian.Uint16(v[n:n+2]))
			}
		}
	}
	p.lifetime.source = uint8(c.number(1))
	switch p.lifetime.source {
	case 1:
		p.lifetime.before = c.number(8)
		p.lifetime.after = c.number(8)
	case 2:
	case 3:
		c.bytes()
	default:
		c.bad = true
	}
	c.extensions()
	p.leafTBS = raw[leafStart : len(raw)-len(c.raw)]
	p.leafSignature = c.bytes()
	c.extensions()
	p.tbs = raw[:len(raw)-len(c.raw)]
	p.signature = c.bytes()
	if c.bad || len(c.raw) != 0 {
		return p, errors.New("invalid MLS key package encoding")
	}
	return p, nil
}

func mlsVector(raw []byte) []byte {
	n := len(raw)
	var result []byte
	switch {
	case n < 64:
		result = []byte{byte(n)}
	case n < 16384:
		result = binary.BigEndian.AppendUint16(nil, uint16(n)|0x4000)
	default:
		result = binary.BigEndian.AppendUint32(nil, uint32(n)|0x80000000)
	}
	return append(result, raw...)
}
func mlsSignContent(label string, raw []byte) []byte {
	return append(mlsVector([]byte("MLS 1.0 "+label)), mlsVector(raw)...)
}
func mlsHash(suite uint16, raw []byte) []byte {
	switch suite {
	case 7:
		h := sha512.Sum384(raw)
		return h[:]
	case 5:
		h := sha512.Sum512(raw)
		return h[:]
	default:
		h := sha256.Sum256(raw)
		return h[:]
	}
}
func verifyMLSSignature(suite uint16, key, raw, signature []byte) bool {
	if suite == 1 {
		return len(key) == ed25519.PublicKeySize && ed25519.Verify(ed25519.PublicKey(key), raw, signature)
	}
	var curve elliptic.Curve
	switch suite {
	case 2:
		curve = elliptic.P256()
	case 7:
		curve = elliptic.P384()
	case 5:
		curve = elliptic.P521()
	default:
		return false
	}
	x, y := elliptic.Unmarshal(curve, key)
	return x != nil && ecdsa.VerifyASN1(&ecdsa.PublicKey{Curve: curve, X: x, Y: y}, mlsHash(suite, raw), signature)
}

func canonicalMLSIdentity(text string, certificate bool) (string, error) {
	var userText, rest string
	if !certificate && strings.HasPrefix(text, "history-client:") {
		user, ok := tokenUUID(strings.TrimPrefix(text, "history-client:"))
		if !ok {
			return "", errors.New("Invalid UUID")
		}
		return "history-client:" + user, nil
	}
	if certificate {
		if !strings.HasPrefix(text, "wireapp://") {
			return "", errors.New("No wireapp label found")
		}
		text = strings.TrimPrefix(text, "wireapp://")
		if len(text) < 25 || text[22:25] != "%21" {
			return "", errors.New("invalid certificate identity")
		}
		uid, err := base64.RawURLEncoding.Strict().DecodeString(text[:22])
		if err != nil || len(uid) != 16 {
			return "", errors.New("Invalid UUID")
		}
		var user gocql.UUID
		copy(user[:], uid)
		userText = user.String()
		rest = text[25:]
	} else {
		if len(text) < 37 || text[36] != ':' {
			return "", errors.New("invalid client identity")
		}
		userText = text[:36]
		rest = text[37:]
	}
	user, ok := tokenUUID(userText)
	if !ok {
		return "", errors.New("Invalid UUID")
	}
	clientText, domainText, ok := strings.Cut(rest, "@")
	if !ok {
		return "", errors.New("invalid client identity")
	}
	client, err := parseClientID(clientText)
	if err != nil {
		return "", errors.New("invalid client identity")
	}
	domain, err := parseEmailDomain(domainText)
	if err != nil {
		return "", errors.New("invalid client identity")
	}
	return user + ":" + client + "@" + domain, nil
}

func (p uploadedMLSPackage) validate(identity string) error {
	if mlsClientScheme(p.suite) == "" {
		return mlsProtocolFailure(fmt.Sprintf("Unsupported ciphersuite 0x%x", p.suite))
	}
	if !verifyMLSSignature(p.suite, p.signatureKey, mlsSignContent("KeyPackageTBS", p.tbs), p.signature) {
		return mlsProtocolFailure("Invalid KeyPackage signature")
	}
	if p.protocol != 1 {
		return mlsProtocolFailure("Unsupported protocol version")
	}
	if !verifyMLSSignature(p.suite, p.signatureKey, mlsSignContent("LeafNodeTBS", p.leafTBS), p.leafSignature) {
		return mlsValidationFailure{"mls-invalid-leaf-node-signature", "Invalid leaf node signature in key package"}
	}
	var actual string
	var err error
	if p.credential == 1 {
		actual, err = canonicalMLSIdentity(string(p.identity), false)
	} else {
		if len(p.certificates) == 0 {
			return mlsProtocolFailure("Failed to parse identity: Invalid x509 certificate chain")
		}
		// Native parses the first certificate and authenticates identity/public
		// key, but does not validate the certificate chain's trust or signature.
		cert, parseErr := x509.ParseCertificate(p.certificates[0])
		if parseErr != nil {
			return mlsProtocolFailure("Failed to parse identity: Failed to decode x509 certificate")
		}
		err = errors.New("No SAN URIs found")
		for _, uri := range cert.URIs {
			value, e := canonicalMLSIdentity(uri.String(), true)
			if e == nil {
				actual = value
				err = nil
				break
			}
			if err.Error() == "No SAN URIs found" {
				err = e
			}
		}
		if err == nil {
			var key []byte
			switch public := cert.PublicKey.(type) {
			case ed25519.PublicKey:
				if p.suite == 1 {
					key = public
				}
			case *ecdsa.PublicKey:
				if p.suite != 1 {
					key = elliptic.Marshal(public.Curve, public.X, public.Y)
				}
			}
			if key == nil {
				scheme := map[uint16]string{1: "Ed25519", 2: "Ecdsa_secp256r1_sha256", 7: "Ecdsa_secp384r1_sha384", 5: "Ecdsa_secp521r1_sha512"}[p.suite]
				return mlsProtocolFailure("Certificate signature scheme " + scheme + " does not match client's public key")
			}
			if !bytes.Equal(key, p.signatureKey) {
				return mlsProtocolFailure("Certificate public key does not match client's")
			}
		}
	}
	if err != nil {
		return mlsProtocolFailure("Failed to parse identity: " + err.Error())
	}
	if actual != identity {
		return mlsProtocolFailure("Client identity does not match credential identity")
	}
	if p.lifetime.source != 1 {
		source := map[uint8]string{2: "update", 3: "commit"}[p.lifetime.source]
		return mlsProtocolFailure("Expected 'key_package' source, got '" + source + "'")
	}
	for _, capability := range p.credentialCapabilities {
		if capability == p.credential {
			return nil
		}
	}
	return mlsProtocolFailure("Missing BasicCredential capability")
}

func (p mlsPackageLifetime) validationError(now time.Time, maximum *mlsLifetimeMaximum) error {
	picos := new(big.Int).Mul(big.NewInt(now.Unix()), big.NewInt(1_000_000_000_000))
	picos.Add(picos, big.NewInt(int64(now.Nanosecond())*1000))
	stamp := func(n uint64) *big.Int {
		return new(big.Int).Mul(new(big.Int).SetUint64(n), big.NewInt(1_000_000_000_000))
	}
	if stamp(p.before).Cmp(picos) > 0 {
		return mlsProtocolFailure("Key package not_before date is in the future")
	}
	if stamp(p.after).Cmp(picos) <= 0 {
		return mlsProtocolFailure("Key package is expired")
	}
	if maximum != nil && stamp(p.after).Cmp(new(big.Int).Add(picos, maximum.picos)) > 0 {
		return mlsProtocolFailure("Key package expiration time is too far in the future")
	}
	return nil
}

type validatedMLSPackage struct {
	suite     uint16
	ref, data []byte
}

func (s mlsClientReads) upload(ctx context.Context, user gocql.UUID, client string, packages []uploadedMLSPackage) error {
	values := make([]validatedMLSPackage, 0, len(packages))
	for _, p := range packages {
		if err := p.validate(user.String() + ":" + client + "@" + s.domain); err != nil {
			return err
		}
		if err := p.lifetime.validationError(s.now(), s.maximum); err != nil {
			return err
		}
		key, present, err := s.key(ctx, user, client, mlsClientScheme(p.suite))
		if err != nil {
			return err
		}
		if !present {
			return mlsProtocolFailure("No key associated to the given identity and signature scheme")
		}
		if !bytes.Equal(key, p.signatureKey) {
			return mlsProtocolFailure("Unrecognised signature key")
		}
		ref := mlsHash(p.suite, append(mlsVector([]byte("MLS 1.0 KeyPackage Reference")), mlsVector(p.raw)...))
		values = append(values, validatedMLSPackage{p.suite, ref, p.raw})
	}
	return s.insert(ctx, user, client, values)
}

func decodeMLSUpload(raw json.RawMessage) ([]uploadedMLSPackage, error) {
	fields, err := registrationObject(raw)
	if err != nil {
		return nil, err
	}
	var values []json.RawMessage
	if string(fields["key_packages"]) == "null" || json.Unmarshal(fields["key_packages"], &values) != nil {
		return nil, errors.New("invalid key package upload")
	}
	result := make([]uploadedMLSPackage, 0, len(values))
	for _, value := range values {
		var text string
		if string(value) == "null" || json.Unmarshal(value, &text) != nil || strings.ContainsAny(text, "\r\n") {
			return nil, errors.New("invalid key package upload")
		}
		data, err := base64.StdEncoding.Strict().DecodeString(text)
		if err != nil {
			return nil, err
		}
		p, err := decodeUploadedMLSPackage(data)
		if err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, nil
}

func (s clientStore) mlsPublicKey(ctx context.Context, user gocql.UUID, client, scheme string) ([]byte, bool, error) {
	var key []byte
	err := s.cassandra.Query(`SELECT key FROM mls_public_keys WHERE user=? AND client=? AND sig_scheme=?`, user, client, scheme).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 1}).Scan(&key)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, false, nil
	}
	return key, err == nil, err
}
func (s clientStore) insertMLSPackages(ctx context.Context, user gocql.UUID, client string, values []validatedMLSPackage) error {
	batch := s.cassandra.NewBatch(gocql.LoggedBatch).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5})
	for _, p := range values {
		batch.Query(`INSERT INTO mls_key_packages (user,client,cipher_suite,data,ref) VALUES (?,?,?,?,?)`, user, client, int32(p.suite), p.data, p.ref)
	}
	return s.cassandra.ExecuteBatch(batch)
}
func (s clientStore) deleteMLSSuite(ctx context.Context, user gocql.UUID, client string, suite uint16) error {
	return s.cassandra.Query(`DELETE FROM mls_key_packages WHERE user=? AND client=? AND cipher_suite=?`, user, client, int32(suite)).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5}).Exec()
}
