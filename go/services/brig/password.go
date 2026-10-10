// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var (
	errPasswordEncoding   = errors.New("invalid stored password encoding")
	errPasswordParameters = errors.New("invalid password hashing parameters")
)

// Password values must never be included in diagnostics or generic formatting.
type passwordHash struct {
	algorithm                                                   string
	version, rounds, memory, iterations, parallelism, blockSize uint32
	salt, key                                                   []byte
}

func (passwordHash) String() string   { return "<Password>" }
func (passwordHash) GoString() string { return "<Password>" }

func parsePasswordHash(encoded string) (passwordHash, error) {
	var h passwordHash
	word := func(s string) (uint32, error) {
		n, err := strconv.ParseUint(s, 10, 32)
		if err != nil {
			return 0, errPasswordEncoding
		}
		return uint32(n), nil
	}
	var err error
	decode := base64.StdEncoding.DecodeString
	parts := strings.Split(encoded, "|")
	if len(parts) == 5 {
		h.algorithm = "scrypt"
		if h.rounds, err = word(parts[0]); err != nil {
			return h, err
		}
		if h.blockSize, err = word(parts[1]); err != nil {
			return h, err
		}
		if h.parallelism, err = word(parts[2]); err != nil {
			return h, err
		}
		if h.salt, err = decode(parts[3]); err != nil {
			return h, errPasswordEncoding
		}
		if h.key, err = decode(parts[4]); err != nil {
			return h, errPasswordEncoding
		}
		return h, nil
	}
	parts = strings.Split(encoded, "$")
	if (len(parts) != 5 && len(parts) != 6) || parts[0] != "" {
		return h, errPasswordEncoding
	}
	h.algorithm = parts[1]
	if h.algorithm != "argon2i" && h.algorithm != "argon2d" && h.algorithm != "argon2id" {
		return h, errPasswordEncoding
	}
	h.version = 16
	i := 2
	if len(parts) == 6 {
		if parts[i] != "v=16" && parts[i] != "v=19" {
			return h, errPasswordEncoding
		}
		h.version, _ = word(strings.TrimPrefix(parts[i], "v="))
		i++
	}
	seen := map[string]bool{}
	for _, parameter := range strings.Split(parts[i], ",") {
		name, value, ok := strings.Cut(parameter, "=")
		if !ok {
			return h, errPasswordEncoding
		}
		n, err := word(value)
		if err != nil {
			return h, err
		}
		switch name {
		case "m":
			h.memory = n
		case "t":
			h.iterations = n
		case "p":
			h.parallelism = n
		default:
			return h, errPasswordEncoding
		}
		seen[name] = true // Native parser uses the last repeated parameter.
	}
	if !seen["m"] || !seen["t"] || !seen["p"] {
		return h, errPasswordEncoding
	}
	pad := func(value string) string { return value + strings.Repeat("=", (4-len(value)%4)%4) }
	if h.salt, err = decode(pad(parts[i+1])); err != nil {
		return h, errPasswordEncoding
	}
	if h.key, err = decode(pad(parts[i+2])); err != nil {
		return h, errPasswordEncoding
	}
	return h, nil
}

func (h passwordHash) derive(plain string) ([]byte, error) {
	if h.algorithm == "scrypt" {
		// The native verifier always derives 64 bytes, irrespective of stored
		// key length. Check integer/arithmetic limits before calling the KDF.
		if h.rounds == 0 || h.rounds >= uint32(strconv.IntSize-1) || h.blockSize == 0 || h.parallelism == 0 || uint64(h.blockSize)*uint64(h.parallelism) >= 1<<30 {
			return nil, errPasswordParameters
		}
		return h.deriveNative(plain)
	}
	if h.version != 19 && h.version != 16 {
		return nil, errPasswordEncoding
	}
	if h.iterations == 0 || h.parallelism == 0 || uint64(h.memory) < 8*uint64(h.parallelism) || len(h.salt) < 8 {
		return nil, errPasswordParameters
	}
	// Native verification fixes output length to 16, including legacy hashes.
	return h.deriveNative(plain)
}

func (h passwordHash) encode() string {
	if h.algorithm == "scrypt" {
		return fmt.Sprintf("%d|%d|%d|%s|%s", h.rounds, h.blockSize, h.parallelism, base64.StdEncoding.EncodeToString(h.salt), base64.StdEncoding.EncodeToString(h.key))
	}
	return fmt.Sprintf("$%s$v=%d$m=%d,t=%d,p=%d$%s$%s", h.algorithm, h.version, h.memory, h.iterations, h.parallelism, base64.RawStdEncoding.EncodeToString(h.salt), base64.RawStdEncoding.EncodeToString(h.key))
}

func verifyPasswordHash(encoded, plain string, argonPolicy bool) (match, needsUpdate bool, err error) {
	h, err := parsePasswordHash(encoded)
	if err != nil {
		return false, false, err
	}
	key, err := h.derive(plain)
	if err != nil {
		return false, false, err
	}
	defer clear(key)
	return subtle.ConstantTimeCompare(key, h.key) == 1, h.algorithm == "scrypt" && argonPolicy, nil
}

func newPasswordHash(plain string, argon bool, memory, iterations, parallelism uint32) (string, error) {
	h := passwordHash{algorithm: "scrypt", rounds: 14, blockSize: 8, parallelism: 1, salt: make([]byte, 32)}
	if argon {
		h = passwordHash{algorithm: "argon2id", version: 19, memory: memory, iterations: iterations, parallelism: parallelism, salt: make([]byte, 16)}
	}
	if _, err := rand.Read(h.salt); err != nil {
		return "", errors.New("password salt generation failed")
	}
	key, err := h.derive(plain)
	if err != nil {
		return "", err
	}
	h.key = key
	return h.encode(), nil
}
