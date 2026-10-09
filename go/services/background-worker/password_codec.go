// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"encoding/base64"
	"errors"
	"math/big"
	"strings"
	"unicode/utf8"
)

// Decode/re-encode only; never derive passwords, allocate KDF work, or log
// hashes. Wire.API.Password.{Argon2id,Scrypt} canonicalize on PG writes.
func canonicalStoredPassword(blob []byte) ([]byte, error) {
	invalid := errors.New("invalid stored password encoding")
	if !utf8.Valid(blob) {
		return nil, invalid
	}
	value := string(blob)
	parts := strings.Split(value, "$")
	if (len(parts) == 5 || len(parts) == 6) && parts[0] == "" && (parts[1] == "argon2i" || parts[1] == "argon2d" || parts[1] == "argon2id") {
		version := "v=16"
		i := 2
		if len(parts) == 6 {
			version = parts[i]
			i++
			if version != "v=16" && version != "v=19" {
				return nil, invalid
			}
		}
		opts := make(map[string]string, 3)
		for _, opt := range strings.Split(parts[i], ",") {
			if len(opt) < 2 || (opt[:2] != "m=" && opt[:2] != "t=" && opt[:2] != "p=") {
				return nil, invalid
			}
			n, ok := haskellWord32(opt[2:])
			if !ok {
				return nil, invalid
			}
			opts[opt[:2]] = n
		}
		if len(opts) != 3 {
			return nil, invalid
		}
		salt, ok := storedBase64(parts[i+1], true)
		if !ok {
			return nil, invalid
		}
		hash, ok := storedBase64(parts[i+2], true)
		if !ok {
			return nil, invalid
		}
		return []byte(strings.Join([]string{"", parts[1], version, "m=" + opts["m="] + ",t=" + opts["t="] + ",p=" + opts["p="], base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(hash)}, "$")), nil
	}
	parts = strings.Split(value, "|")
	if len(parts) != 5 {
		return nil, invalid
	}
	for i := 0; i < 3; i++ {
		n, ok := haskellWord32(parts[i])
		if !ok {
			return nil, invalid
		}
		parts[i] = n
	}
	salt, ok := storedBase64(parts[3], false)
	if !ok {
		return nil, invalid
	}
	hash, ok := storedBase64(parts[4], false)
	if !ok {
		return nil, invalid
	}
	parts[3] = base64.StdEncoding.EncodeToString(salt)
	parts[4] = base64.StdEncoding.EncodeToString(hash)
	return []byte(strings.Join(parts, "|")), nil
}

func storedBase64(value string, pad bool) ([]byte, bool) {
	// Native base64-bytestring decode rejects whitespace. Go's decoder skips
	// CR/LF, so reject those explicitly rather than accepting malformed rows.
	if strings.ContainsAny(value, "\r\n") {
		return nil, false
	}
	if pad {
		value += strings.Repeat("=", (4-len(value)%4)%4)
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(value)
	return decoded, err == nil
}

// Native readT uses Read Word32: surrounding whitespace/parentheses and
// decimal/hex/octal integer literals are allowed; fromInteger wraps to 32 bits.
func haskellWord32(value string) (string, bool) {
	value = strings.TrimSpace(value)
	for strings.HasPrefix(value, "(") && strings.HasSuffix(value, ")") {
		value = strings.TrimSpace(value[1 : len(value)-1])
	}
	negative := strings.HasPrefix(value, "-")
	if negative {
		value = strings.TrimSpace(value[1:])
	}
	base := 10
	if strings.HasPrefix(value, "0x") || strings.HasPrefix(value, "0X") {
		base = 16
		value = value[2:]
	} else if strings.HasPrefix(value, "0o") || strings.HasPrefix(value, "0O") {
		base = 8
		value = value[2:]
	}
	if value == "" {
		return "", false
	}
	for _, c := range value {
		if !(c >= '0' && c <= '9' || base == 16 && (c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F')) {
			return "", false
		}
	}
	n, ok := new(big.Int).SetString(value, base)
	if !ok {
		return "", false
	}
	if negative {
		n.Neg(n)
	}
	n.Mod(n, new(big.Int).Lsh(big.NewInt(1), 32))
	return n.String(), true
}
