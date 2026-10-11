// SPDX-License-Identifier: AGPL-3.0-only
package brig

import "testing"

func TestConferenceCallingNativeNullFallbackAndStoredPrecedence(t *testing.T) {
	c, err := LoadConfig("../../../services/brig/brig.integration.yaml")
	if err != nil {
		t.Fatal(err)
	}
	newStatus, err := conferenceCallingNew(c)
	if err != nil || newStatus == nil || *newStatus != 0 {
		t.Fatal("native registration default changed", err)
	}
	if absent, err := conferenceCallingNew(Config{}); err != nil || absent != nil {
		t.Fatal("absent registration feature must not write", err)
	}
	status, err := conferenceCallingStatus(c, nil)
	if err != nil || status != "disabled" {
		t.Fatal("native configured null fallback changed")
	}
	status, err = conferenceCallingStatus(Config{}, nil)
	if err != nil || status != "enabled" {
		t.Fatal("native absent feature flags fallback changed")
	}
	for _, value := range []int32{0, 1} {
		status, err = conferenceCallingStatus(c, &value)
		want := "disabled"
		if value == 1 {
			want = "enabled"
		}
		if err != nil || status != want {
			t.Fatal("stored feature status did not override configured default")
		}
	}
	invalid := int32(2)
	if _, err = conferenceCallingStatus(c, &invalid); err == nil {
		t.Fatal("invalid stored status accepted")
	}
	c.Settings.FeatureFlags.ConferenceCalling.DefaultForNull.Status = "invalid"
	if c.Validate() == nil {
		t.Fatal("invalid configured feature status accepted")
	}
	c.Settings.FeatureFlags.ConferenceCalling.DefaultForNull.Status = "disabled"
	c.Settings.FeatureFlags.ConferenceCalling.DefaultForNew.Status = "invalid"
	if c.Validate() == nil {
		t.Fatal("invalid registration feature status accepted")
	}
}
