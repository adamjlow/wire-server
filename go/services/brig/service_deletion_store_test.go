// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"reflect"
	"testing"
)

func syntheticDeletionServiceRow() deletionServiceRow {
	name, description := "Synthetic service", "Synthetic description"
	typ, size := int32(0), int32(2048)
	enabled := false
	return deletionServiceRow{name: &name, description: &description, baseURL: []byte("https://example.com/service"), tokens: []string{"synthetic"}, keys: []deletionServiceKey{{Type: &typ, Size: &size, PEM: []byte("-----BEGIN PUBLIC KEY-----\nAQID\n-----END PUBLIC KEY-----\n")}}, tags: []int64{1, 4, 1 << 30}, enabled: &enabled}
}

func TestServiceDeletionFullNativeRowAdmission(t *testing.T) {
	if err := syntheticDeletionServiceRow().validate(); err != nil {
		t.Fatal("native optional summary/default assets", err)
	}
	for _, test := range []struct {
		name   string
		change func(*deletionServiceRow)
	}{
		{"name", func(r *deletionServiceRow) { r.name = nil }},
		{"description", func(r *deletionServiceRow) { r.description = nil }},
		{"enabled", func(r *deletionServiceRow) { r.enabled = nil }},
		{"http", func(r *deletionServiceRow) { r.baseURL = []byte("http://example.com") }},
		{"relative", func(r *deletionServiceRow) { r.baseURL = []byte("/service") }},
		{"url-space", func(r *deletionServiceRow) { r.baseURL = []byte("https://example.com/a b") }},
		{"tokens", func(r *deletionServiceRow) { r.tokens = nil }},
		{"keys", func(r *deletionServiceRow) { r.keys = nil }},
		{"pem", func(r *deletionServiceRow) { r.keys[0].PEM = []byte("invalid synthetic pem") }},
		{"two-pem", func(r *deletionServiceRow) { r.keys[0].PEM = append(r.keys[0].PEM, r.keys[0].PEM...) }},
		{"key-type", func(r *deletionServiceRow) { v := int32(1); r.keys[0].Type = &v }},
		{"key-size", func(r *deletionServiceRow) { r.keys[0].Size = nil }},
		{"tag-enum", func(r *deletionServiceRow) { r.tags = []int64{3} }},
		{"tag-negative", func(r *deletionServiceRow) { r.tags = []int64{-1} }},
		{"tag-high", func(r *deletionServiceRow) { r.tags = []int64{1 << 31} }},
		{"asset", func(r *deletionServiceRow) { r.assets = []deletionServiceAsset{{}} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			row := syntheticDeletionServiceRow()
			test.change(&row)
			if row.validate() == nil {
				t.Fatal("invalid native row accepted")
			}
		})
	}
}

func TestServiceDeletionTagSubsetsAndNativeLowercase(t *testing.T) {
	got, err := deletionServiceTagMasks([]int64{4, 1, 2, 1})
	if err != nil || !reflect.DeepEqual(got, []int64{1, 2, 3, 4, 5, 6, 7}) {
		t.Fatal("native subset index keys", got, err)
	}
	if got, err := deletionServiceTagMasks(nil); err != nil || len(got) != 0 {
		t.Fatal("empty native tag set", got, err)
	}
	for _, tags := range [][]int64{{1, 2, 4, 8}, {0}, {3}, {-1}, {1 << 31}} {
		if _, err := deletionServiceTagMasks(tags); err == nil {
			t.Fatal("invalid tag combinations admitted")
		}
	}
	for _, test := range []struct{ in, out string }{{"İΣ", "i\u0307σ"}, {"ΟΣ", "οσ"}, {"Synthetic", "synthetic"}, {"", ""}} {
		if got := deletionServiceLower(test.in); got != test.out {
			t.Fatalf("native lowercase %q -> %q want %q", test.in, got, test.out)
		}
	}
}
