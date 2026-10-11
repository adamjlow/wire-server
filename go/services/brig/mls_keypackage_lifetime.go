// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"errors"
	"math/big"
	"time"

	"go.yaml.in/yaml/v3"
)

// This is the native syntactic MLS decoder used by counting, not upload-time
// signature, credential, cipher-suite or public-key validation.
type mlsCursor struct {
	raw []byte
	bad bool
}

func (c *mlsCursor) take(n int) []byte {
	if n < 0 || n > len(c.raw) {
		c.bad = true
		return nil
	}
	raw := c.raw[:n]
	c.raw = c.raw[n:]
	return raw
}
func (c *mlsCursor) number(n int) uint64 {
	var value uint64
	for _, b := range c.take(n) {
		value = value<<8 | uint64(b)
	}
	return value
}
func (c *mlsCursor) bytes() []byte {
	if len(c.raw) == 0 {
		c.bad = true
		return nil
	}
	prefix := c.raw[0] >> 6
	if prefix == 3 {
		c.bad = true
		return nil
	}
	n := 1 << prefix
	value := c.number(n) & ((uint64(1) << (uint(n)*8 - 2)) - 1)
	if c.bad || value > uint64(len(c.raw)) {
		c.bad = true
		return nil
	}
	return c.take(int(value))
}
func (c *mlsCursor) extensions() {
	v := mlsCursor{raw: c.bytes()}
	for len(v.raw) > 0 && !v.bad {
		v.take(2)
		v.bytes()
	}
	c.bad = c.bad || v.bad
}

type mlsPackageLifetime struct {
	source        uint8
	before, after uint64
}

func decodeMLSPackageLifetime(raw []byte) (mlsPackageLifetime, bool) {
	c := mlsCursor{raw: raw}
	c.take(4)
	c.bytes() // protocol, suite, init key
	c.bytes()
	c.bytes() // leaf encryption and signature keys
	switch c.number(2) {
	case 1:
		c.bytes()
	case 2:
		v := mlsCursor{raw: c.bytes()}
		for len(v.raw) > 0 && !v.bad {
			v.bytes()
		}
		c.bad = c.bad || v.bad
	default:
		c.bad = true
	}
	for range 5 {
		if len(c.bytes())%2 != 0 {
			c.bad = true
		}
	} // capability Word16 vectors
	lifetime := mlsPackageLifetime{source: uint8(c.number(1))}
	switch lifetime.source {
	case 1:
		lifetime.before = c.number(8)
		lifetime.after = c.number(8)
	case 2:
	case 3:
		c.bytes()
	default:
		c.bad = true
	}
	c.extensions()
	c.bytes()
	c.extensions()
	c.bytes() // leaf extensions/signature, package extensions/signature
	return lifetime, !c.bad && len(c.raw) == 0
}

type mlsLifetimeMaximum struct{ picos *big.Int }

func (m *mlsLifetimeMaximum) UnmarshalYAML(n *yaml.Node) error {
	if n.Tag != "!!int" && n.Tag != "!!float" {
		return errors.New("invalid MLS maximum lifetime")
	}
	r, ok := new(big.Rat).SetString(n.Value)
	if !ok {
		return errors.New("invalid MLS maximum lifetime")
	}
	r.Mul(r, big.NewRat(1_000_000_000_000, 1))
	m.picos = new(big.Int).Div(r.Num(), r.Denom())
	return nil
}
func (p mlsPackageLifetime) usable(now time.Time, maximum *mlsLifetimeMaximum) bool {
	if p.source != 1 {
		return false
	}
	picos := new(big.Int).Mul(big.NewInt(now.Unix()), big.NewInt(1_000_000_000_000))
	picos.Add(picos, big.NewInt(int64(now.Nanosecond())*1000))
	timestamp := func(n uint64) *big.Int {
		return new(big.Int).Mul(new(big.Int).SetUint64(n), big.NewInt(1_000_000_000_000))
	}
	if timestamp(p.before).Cmp(picos) > 0 || timestamp(p.after).Cmp(picos) <= 0 {
		return false
	}
	return maximum == nil || timestamp(p.after).Cmp(new(big.Int).Add(picos, maximum.picos)) <= 0
}
