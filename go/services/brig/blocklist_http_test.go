// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"net/http/httptest"
	"testing"
)

func TestNativeBlacklistResponseBodiesAndMedia(t *testing.T) {
	for _, method := range []string{"GET", "HEAD"} {
		for _, found := range []bool{false, true} {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(method, "/i/users/blacklist?email=synthetic@example.com", nil)
			blacklistReadResponse(w, r, found)
			status := 404
			if found {
				status = 200
			}
			body := "[]"
			if method == "HEAD" {
				body = ""
			}
			if w.Code != status || w.Header().Get("Content-Type") != "application/json;charset=utf-8" || w.Body.String() != body {
				t.Fatal("observed native blacklist response changed", method, found, w.Code, w.Header(), w.Body.String())
			}
		}
	}
	w := httptest.NewRecorder()
	registrationHTTPError(w, errRegistrationBlacklisted)
	if w.Code != 403 || w.Header().Get("Content-Type") != "application/json;charset=utf-8" {
		t.Fatal("observed native blacklisted registration error media changed")
	}
}
