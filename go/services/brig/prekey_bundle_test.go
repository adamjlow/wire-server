// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"encoding/base64"
	"testing"
)

func TestNativePrekeyFixtureAndClientID(t *testing.T) {
	// Wire.API.User.Client.Prekey.fakeLastPrekey: synthetic, intentionally ID7.
	key := uncheckedPrekey{ID: 7, Key: "pQABAQcCoQBYIDXdN8VlKb5lbgPmoDPLPyqNIEyShG4oT/DlW0peRRZUA6EAoQBYILLf1TIwSB62q69Ojs/X1tzJ+dYHNAw4QbW/7TC5vSZqBPY="}
	if !checkPrekeyBundle(key) {
		t.Fatal("native fixture rejected")
	}
	id := clientIDFromPrekey(key)
	if id != "cc0abaacc31e2926" {
		t.Fatal("native text-hash client ID differs", id)
	}
	key.ID = 65535
	if checkPrekeyBundle(key) {
		t.Fatal("mismatched last-key ID accepted")
	}
}

func TestPrekeyCBORSchemaAndNativeIntegerTruncation(t *testing.T) {
	valid := []byte{0xa4, 0, 1, 1, 7, 2, 0xa1, 0, 0x40, 3, 0xa1, 0, 0xa1, 0, 0x40}
	for _, data := range [][]byte{valid, append(append([]byte{}, valid[:5]...), valid[5:]...)} {
		id, err := parsePrekeyID(base64.StdEncoding.EncodeToString(data))
		if err != nil || id != 7 {
			t.Fatal(id, err)
		}
	}
	wrapped := append(append([]byte{}, valid[:4]...), 0x1a, 0, 1, 0, 7)
	wrapped = append(wrapped, valid[5:]...)
	if id, err := parsePrekeyID(base64.StdEncoding.EncodeToString(wrapped)); err != nil || id != 7 {
		t.Fatal("native Word16 truncation lost", id, err)
	}
	duplicate := append([]byte{0xa5}, valid[1:]...)
	duplicate = append(duplicate, 1, 8)
	if id, err := parsePrekeyID(base64.StdEncoding.EncodeToString(duplicate)); err != nil || id != 8 {
		t.Fatal("duplicate CBOR keys must retain last", id, err)
	}
	for _, tail := range [][]byte{{4, 0xf6}, {4, 0xa1, 0, 0x40}} {
		data := append([]byte{0xa5}, valid[1:]...)
		data = append(data, tail...)
		if _, err := parsePrekeyID(base64.StdEncoding.EncodeToString(data)); err != nil {
			t.Fatal(err)
		}
	}
	for _, data := range [][]byte{
		{}, {0xbf}, valid[:len(valid)-1], append(append([]byte{}, valid...), 0),
		append([]byte{0xa3}, valid[1:]...),
		{0xa4, 0, 1, 1, 0x20, 2, 0xa1, 0, 0x40, 3, 0xa1, 0, 0xa1, 0, 0x40},
		{0xa4, 0, 1, 1, 7, 2, 0xa1, 1, 0x40, 3, 0xa1, 0, 0xa1, 0, 0x40},
		{0xa4, 0, 1, 1, 7, 2, 0xa1, 0, 0x40, 3, 0xa1, 0, 0x40},
		append([]byte{0xa5}, append(valid[1:], 5, 0)...),
		append([]byte{0xa5}, append(valid[1:], 4, 0xa1, 0, 0x5f, 0xff)...),
	} {
		if _, err := parsePrekeyID(base64.StdEncoding.EncodeToString(data)); err == nil {
			t.Fatalf("malformed CBOR accepted: %x", data)
		}
	}
	for _, encoded := range []string{"!!", base64.StdEncoding.EncodeToString(valid) + "\n", base64.StdEncoding.EncodeToString(valid[:len(valid)-1])[:2]} {
		if _, err := parsePrekeyID(encoded); err == nil {
			t.Fatal("malformed Base64 accepted")
		}
	}
}
