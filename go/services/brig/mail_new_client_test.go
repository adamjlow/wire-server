// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"strings"
	"testing"
	"time"
)

func TestNativeNewClientMailCalendarsAndTemplate(t *testing.T) {
	at := time.Date(2023, 3, 5, 13, 4, 0, 0, time.UTC)
	for _, tc := range []struct{ locale, want string }{
		{"en", "Sunday  5 March 2023, 13:04 - UTC"}, {"de-DE", "Sonntag  5 März 2023, 13:04 - UTC"}, {"fr", "dimanche  5 mars 2023, 13:04 - UTC"}, {"es", "Sunday  5 March 2023, 13:04 - UTC"},
	} {
		if nativeNewClientDate(at, tc.locale) != tc.want {
			t.Fatal("native calendar/date format differs", tc.locale)
		}
	}
	renderer, err := newRegistrationMailRenderer(nativeMailTestConfig(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	email := "synthetic@example.com"
	profile := selfProfile{Name: "Synthetic", Email: &email, Locale: "en"}
	mail, err := renderer.newClient(profile, storedClient{Created: at})
	if err != nil {
		t.Fatal(err)
	}
	if mail.to != email || mail.toName == nil || *mail.toName != "Synthetic" || len(mail.headers) != 1 || mail.headers[0] != [2]string{"X-Zeta-Purpose", "NewDevice"} || !strings.Contains(mail.plain, "N/A") || !strings.Contains(mail.plain, "Sunday  5 March 2023, 13:04 - UTC") {
		t.Fatal("native new-client template/defaults/envelope differ")
	}
}
