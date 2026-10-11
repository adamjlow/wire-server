// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"strconv"
	"strings"
)

type uncheckedPrekey struct {
	ID  uint16 `json:"id"`
	Key string `json:"key"`
}

func (uncheckedPrekey) String() string { return "<PrekeyBundle>" }

var errPrekeyBundle = errors.New("malformed prekey bundle")

// This codec matches Wire.API.User.Client.Prekey's narrow CBOR grammar.
// It deliberately does not impose cryptographic key lengths or verification
// that the reference's checkPrekeyBundle does not perform.
type prekeyCBOR struct {
	data []byte
	at   int
}

func (c *prekeyCBOR) head() (byte, uint64, error) {
	if c.at >= len(c.data) {
		return 0, 0, errPrekeyBundle
	}
	b := c.data[c.at]
	c.at++
	major, extra := b>>5, b&31
	if extra < 24 {
		return major, uint64(extra), nil
	}
	width := 0
	switch extra {
	case 24:
		width = 1
	case 25:
		width = 2
	case 26:
		width = 4
	case 27:
		width = 8
	default:
		return 0, 0, errPrekeyBundle
	}
	if len(c.data)-c.at < width {
		return 0, 0, errPrekeyBundle
	}
	var n uint64
	for _, b := range c.data[c.at : c.at+width] {
		n = n<<8 | uint64(b)
	}
	c.at += width
	return major, n, nil
}

func (c *prekeyCBOR) expect(major byte) (uint64, error) {
	tag, n, err := c.head()
	if err != nil || tag != major {
		return 0, errPrekeyBundle
	}
	return n, nil
}

func (c *prekeyCBOR) int() (int64, error) {
	tag, n, err := c.head()
	if err != nil || tag > 1 || n > uint64(^uint64(0)>>1) {
		return 0, errPrekeyBundle
	}
	if tag == 1 {
		return -1 - int64(n), nil
	}
	return int64(n), nil
}

func (c *prekeyCBOR) singleton() error {
	n, err := c.expect(5)
	if err != nil || n != 1 {
		return errPrekeyBundle
	}
	k, err := c.int()
	if err != nil || k != 0 {
		return errPrekeyBundle
	}
	return nil
}

func (c *prekeyCBOR) bytes() error {
	n, err := c.expect(2)
	if err != nil || n > uint64(len(c.data)-c.at) {
		return errPrekeyBundle
	}
	c.at += int(n)
	return nil
}

func parsePrekeyID(encoded string) (uint16, error) {
	// Go's Base64 decoder ignores CR/LF; the native strict decoder rejects them.
	if strings.ContainsAny(encoded, "\r\n") {
		return 0, errPrekeyBundle
	}
	data, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil {
		return 0, errPrekeyBundle
	}
	c := prekeyCBOR{data: data}
	n, err := c.expect(5)
	if err != nil || n > uint64(len(data)-c.at) {
		return 0, errPrekeyBundle
	}
	var seen [4]bool
	var id uint16
	for range n {
		key, err := c.int()
		if err != nil {
			return 0, errPrekeyBundle
		}
		switch key {
		case 0:
			if _, err := c.expect(0); err != nil {
				return 0, err
			}
			seen[0] = true
		case 1:
			value, err := c.int()
			if err != nil || value < 0 {
				return 0, errPrekeyBundle
			}
			id = uint16(value)
			seen[1] = true
		case 2, 3:
			if c.singleton() != nil {
				return 0, errPrekeyBundle
			}
			if key == 3 && c.singleton() != nil {
				return 0, errPrekeyBundle
			}
			if c.bytes() != nil {
				return 0, errPrekeyBundle
			}
			seen[key] = true
		case 4:
			if c.at < len(data) && data[c.at] == 0xf6 {
				c.at++
				continue
			}
			if c.singleton() != nil || c.bytes() != nil {
				return 0, errPrekeyBundle
			}
		default:
			return 0, errPrekeyBundle
		}
	}
	if !seen[0] || !seen[1] || !seen[2] || !seen[3] || c.at != len(data) {
		return 0, errPrekeyBundle
	}
	return id, nil
}

func checkPrekeyBundle(key uncheckedPrekey) bool {
	id, err := parsePrekeyID(key.Key)
	return err == nil && id == key.ID
}

func clientIDFromPrekey(key uncheckedPrekey) string {
	digest := sha256.Sum256([]byte(key.Key))
	return strconv.FormatUint(binary.BigEndian.Uint64(digest[:8]), 16)
}
