// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"bytes"
	"cmp"
	"slices"
	"strconv"
)

func compareClientOptional[T cmp.Ordered](a, b *T) int {
	if a == nil {
		if b == nil {
			return 0
		}
		return -1
	}
	if b == nil {
		return 1
	}
	return cmp.Compare(*a, *b)
}

func clientCapabilitySet(values []int32) []int32 {
	result := append([]int32{}, values...)
	slices.Sort(result)
	return slices.Compact(result)
}

// Native Set Client uses its derived record Ord, beginning with Word64 client
// IDs, not lexical hex order. Keep all record fields for legacy ID aliases.
func compareNativeClients(a, b storedClient) int {
	ai, _ := strconv.ParseUint(a.ID, 16, 64)
	bi, _ := strconv.ParseUint(b.ID, 16, 64)
	for _, order := range []int{cmp.Compare(ai, bi), cmp.Compare(a.Type, b.Type), a.Created.Compare(b.Created), compareClientOptional(a.Class, b.Class), compareClientOptional(a.Label, b.Label), compareClientOptional(a.Cookie, b.Cookie), compareClientOptional(a.Model, b.Model), slices.Compare(clientCapabilitySet(a.Capabilities), clientCapabilitySet(b.Capabilities))} {
		if order != 0 {
			return order
		}
	}
	keys := func(values map[string][]byte) []string {
		result := make([]string, 0, len(values))
		for key := range values {
			result = append(result, key)
		}
		// All four supported scheme names sort in their native numeric tag
		// order: ECDSA P256/P384/P521 followed by Ed25519.
		slices.Sort(result)
		return result
	}
	ak, bk := keys(a.PublicKeys), keys(b.PublicKeys)
	for i := 0; i < min(len(ak), len(bk)); i++ {
		if order := cmp.Compare(ak[i], bk[i]); order != 0 {
			return order
		}
		if order := bytes.Compare(a.PublicKeys[ak[i]], b.PublicKeys[bk[i]]); order != 0 {
			return order
		}
	}
	if order := cmp.Compare(len(ak), len(bk)); order != 0 {
		return order
	}
	if a.LastActive == nil {
		if b.LastActive == nil {
			return 0
		}
		return -1
	}
	if b.LastActive == nil {
		return 1
	}
	return a.LastActive.Compare(*b.LastActive)
}

func nativeClientSet(clients []storedClient) ([]storedClient, error) {
	result := append([]storedClient{}, clients...)
	for i := range result {
		if err := result[i].validate(); err != nil {
			return nil, err
		}
		id, err := parseClientID(result[i].ID)
		if err != nil {
			return nil, err
		}
		result[i].ID = id
	}
	slices.SortFunc(result, compareNativeClients)
	return slices.CompactFunc(result, func(a, b storedClient) bool { return compareNativeClients(a, b) == 0 }), nil
}
