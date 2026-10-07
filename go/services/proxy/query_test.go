// SPDX-License-Identifier: AGPL-3.0-or-later
package proxy

import "testing"

func TestProviderQueryMatchesCapturedHaskellBytes(t *testing.T) {
	for _, test := range []struct{ input, want string }{
		{"q=one+two&tag=a&tag=b&bare&ACCESS_TOKEN=discard&access_token=discard2", "api_key=synthetic-giphy-secret&q=one%20two&tag=a&tag=b&bare"},
		{"key=caller&api_key=caller&key=again&x=%2F%FF", "api_key=synthetic-giphy-secret&key=caller&api_key=caller&key=again&x=%2F%FF"},
		{"q=%26%3D", "api_key=synthetic-giphy-secret&q=%26%3D"},
	} {
		if got := providerQuery(test.input, "api_key", "synthetic-giphy-secret"); got != test.want {
			t.Fatalf("query differs from independent Haskell capture: %q", got)
		}
	}
}

func TestQueryDistinctionsAndInjectionEncoding(t *testing.T) {
	got := providerQuery("empty=&bare&k=one&k=two&%41ccess_token=discard", "key", "synthetic\r\n&= +")
	want := "key=synthetic%0D%0A%26%3D%20%2B&empty=&bare&k=one&k=two"
	if got != want {
		t.Fatalf("query distinctions/encoding lost: %q", got)
	}
	if decoded := decodeQueryBytes("%GG%20%FF+"); decoded != "%GG \xff " {
		t.Fatalf("opaque percent bytes lost: %q", decoded)
	}
	if got := providerQuery("access_to%E2%84%AAen=keep", "key", "synthetic"); got != "key=synthetic&access_to%E2%84%AAen=keep" {
		t.Fatal("Unicode lookalike incorrectly treated as an ASCII credential key")
	}
}
