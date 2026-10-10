// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"math/big"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrInvalidToken   = errors.New("invalid zauth token")
	ErrFalsifiedToken = errors.New("falsified zauth token")
	ErrExpiredToken   = errors.New("expired zauth token")
)

// ZToken preserves Data.ZAuth.Token's parsed representation. In particular,
// validation signs canonical parsed fields, not the supplied property order.
// A client field is optional, including the distinct present-but-empty value.
type ZToken struct {
	Version, Key                      int64
	Expires                           big.Int
	Kind                              string
	Session                           bool
	User, Provider, Bot, Conversation string
	Client                            *string
	Connection                        uint64
	Random                            uint32
	Signature                         []byte
}

func tokenUUID(s string) (string, bool) {
	if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return "", false
	}
	if b, err := hex.DecodeString(strings.ReplaceAll(s, "-", "")); err != nil || len(b) != 16 {
		return "", false
	}
	return strings.ToLower(s), true
}

func ParseZToken(value, kind string) (ZToken, error) {
	var t ZToken
	parts := strings.Split(value, ".")
	if len(parts) < 2 || strings.ContainsAny(parts[0], "\r\n") {
		return t, ErrInvalidToken
	}
	sig, err := base64.URLEncoding.Strict().DecodeString(parts[0])
	if err != nil {
		return t, ErrInvalidToken
	}
	properties := make(map[string]string, len(parts)-1)
	for _, part := range parts[1:] {
		k, v, _ := strings.Cut(part, "=")
		if _, exists := properties[k]; !exists {
			properties[k] = v
		}
	}
	t.Kind = properties["t"]
	if t.Kind != kind || (kind != "a" && kind != "u" && kind != "la" && kind != "lu" && kind != "b" && kind != "p") {
		return t, ErrInvalidToken
	}
	if t.Version, err = strconv.ParseInt(properties["v"], 10, 64); err != nil {
		return t, ErrInvalidToken
	}
	if t.Key, err = strconv.ParseInt(properties["k"], 10, 64); err != nil {
		return t, ErrInvalidToken
	}
	if _, ok := t.Expires.SetString(properties["d"], 10); !ok {
		return t, ErrInvalidToken
	}
	t.Session = properties["l"] == "s"
	if client, exists := properties["i"]; exists && utf8.ValidString(client) {
		t.Client = &client
	}
	var ok bool
	switch kind {
	case "a", "la", "u", "lu":
		if t.User, ok = tokenUUID(properties["u"]); !ok {
			return t, ErrInvalidToken
		}
		if kind == "a" || kind == "la" {
			if t.Connection, err = strconv.ParseUint(properties["c"], 10, 64); err != nil {
				return t, ErrInvalidToken
			}
		} else {
			random, e := strconv.ParseUint(properties["r"], 16, 32)
			if e != nil {
				return t, ErrInvalidToken
			}
			t.Random = uint32(random)
		}
	case "b":
		if t.Provider, ok = tokenUUID(properties["p"]); !ok {
			return t, ErrInvalidToken
		}
		if t.Bot, ok = tokenUUID(properties["b"]); !ok {
			return t, ErrInvalidToken
		}
		if t.Conversation, ok = tokenUUID(properties["c"]); !ok {
			return t, ErrInvalidToken
		}
	case "p":
		if t.Provider, ok = tokenUUID(properties["p"]); !ok {
			return t, ErrInvalidToken
		}
	}
	t.Signature = sig
	return t, nil
}

func (t ZToken) canonical() ([]byte, error) {
	tag := ""
	if t.Session {
		tag = "s"
	}
	data := "v=" + strconv.FormatInt(t.Version, 10) + ".k=" + strconv.FormatInt(t.Key, 10) + ".d=" + t.Expires.String() + ".t=" + t.Kind + ".l=" + tag
	client := ""
	if t.Client != nil {
		if !utf8.ValidString(*t.Client) {
			return nil, ErrInvalidToken
		}
		client = ".i=" + *t.Client
	}
	uuid := func(s string) (string, error) {
		u, ok := tokenUUID(s)
		if !ok {
			return "", ErrInvalidToken
		}
		return u, nil
	}
	switch t.Kind {
	case "a", "la", "u", "lu":
		u, err := uuid(t.User)
		if err != nil {
			return nil, err
		}
		data += ".u=" + u
		if t.Kind == "a" || t.Kind == "la" {
			data += client + ".c=" + strconv.FormatUint(t.Connection, 10)
		} else {
			data += ".r=" + strconv.FormatUint(uint64(t.Random), 16) + client
		}
	case "b":
		p, err := uuid(t.Provider)
		if err != nil {
			return nil, err
		}
		b, err := uuid(t.Bot)
		if err != nil {
			return nil, err
		}
		c, err := uuid(t.Conversation)
		if err != nil {
			return nil, err
		}
		data += ".p=" + p + ".b=" + b + ".c=" + c
	case "p":
		p, err := uuid(t.Provider)
		if err != nil {
			return nil, err
		}
		data += ".p=" + p
	default:
		return nil, ErrInvalidToken
	}
	return []byte(data), nil
}

func (t ZToken) Encode() (string, error) {
	data, err := t.canonical()
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(t.Signature) + "." + string(data), nil
}

func (t *ZToken) Sign(key ed25519.PrivateKey) error {
	if len(key) != ed25519.PrivateKeySize {
		return ErrInvalidToken
	}
	data, err := t.canonical()
	if err != nil {
		return err
	}
	t.Signature = ed25519.Sign(key, data)
	return nil
}

func (t ZToken) Verify(keys []ed25519.PublicKey, now time.Time) error {
	if t.Key < 1 || t.Key > int64(len(keys)) || len(keys[t.Key-1]) != ed25519.PublicKeySize {
		return ErrInvalidToken
	}
	data, err := t.canonical()
	if err != nil {
		return err
	}
	if len(t.Signature) != ed25519.SignatureSize || !ed25519.Verify(keys[t.Key-1], data, t.Signature) {
		return ErrFalsifiedToken
	}
	if t.Expires.Cmp(big.NewInt(-1)) != 0 && t.Expires.Cmp(big.NewInt(now.Unix())) < 0 {
		return ErrExpiredToken
	}
	return nil
}
