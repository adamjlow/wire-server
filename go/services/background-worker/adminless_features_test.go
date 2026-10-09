// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"encoding/json"
	"math/big"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestAdminlessNativeTimeoutArithmeticAndGrammar(t *testing.T) {
	for _, tc := range []struct{ text, pico string }{{"7d", "604800000000000000"}, {"+1.e2s", "100000000000000"}, {"1e-3s", "1000000000"}, {"0.0000000000009s", "0"}, {"1.0000000000009us", "1000000"}, {"1.0000019us", "1000001"}, {"-0s", "0"}, {"1e-999999s", "0"}, {"18446744073709551615d", "1593798687968505259536000000000000000"}} {
		got, err := parseAdminlessDuration(tc.text)
		if err != nil || got.String() != tc.pico {
			t.Fatalf("%q pico=%v error=%v want=%s", tc.text, got, err, tc.pico)
		}
	}
	for _, text := range []string{".5s", " 1s", "1s ", "1S", "1s2s", "1es", "-1s", "-0.0000000000001us", "18446744073709551616d", "1e999999s", "NaNs", "Infh"} {
		if _, err := parseAdminlessDuration(text); err == nil {
			t.Fatalf("invalid native duration %q", text)
		}
	}
}
func TestAdminlessDefaultsAndDatabasePatchSemantics(t *testing.T) {
	defaults, err := decodeAdminlessDefaults(nil)
	if err != nil || defaults.Feature.Enabled || defaults.Locked || defaults.Feature.Strategy != "alphabetical" {
		t.Fatal("native default mismatch")
	}
	modern := json.RawMessage(`{"defaults":{"status":"enabled","lockStatus":"unlocked","config":{"promotionStrategy":"random","deletionTimeout":9,"deletionTimeoutDuration":"1s","reminderTimeouts":[3],"reminderTimeoutDurations":[]},"ttl":"unlimited"}}`)
	defaults, err = decodeAdminlessDefaults(modern)
	if err != nil || !defaults.Feature.Enabled || defaults.Feature.DeletionPico.Cmp(big.NewInt(1000000000000)) != 0 || len(defaults.Feature.ReminderPico) != 0 {
		t.Fatalf("modern precedence %+v %v", defaults, err)
	}
	legacy, err := decodeAdminlessDefaults(json.RawMessage(`{"defaults":{"status":"disabled","lockStatus":"locked","config":{"promotionStrategy":"all","deletionTimeout":7e0,"reminderTimeouts":[2,4,6]}}}`))
	if err != nil || !legacy.Locked || legacy.Feature.DeletionPico.Cmp(defaultAdminlessFeature().Feature.DeletionPico) != 0 {
		t.Fatal("legacy server days not decoded")
	}
	enabled, unlocked, locked := int32(1), int32(1), int32(0)
	row := &featureMigrationRow{Status: &enabled, LockStatus: &unlocked, Config: json.RawMessage(`{"promotionStrategy":"all","deletionTimeout":17,"reminderTimeouts":[1]}`)}
	got, err := resolveAdminlessFeature(defaults, row)
	if err != nil || got.Strategy != "all" || got.DeletionPico.Cmp(defaults.Feature.DeletionPico) != 0 || len(got.ReminderPico) != 0 {
		t.Fatalf("stored legacy fields must not override %v %v", got, err)
	}
	row.LockStatus = &locked
	got, err = resolveAdminlessFeature(legacy, row)
	if err != nil || got.Enabled || got.Strategy != "all" {
		t.Fatal("locked feature did not revert server defaults")
	}
	row.Config = json.RawMessage(`{"deletionTimeoutDuration":"invalid"}`)
	if _, err := resolveAdminlessFeature(legacy, row); err == nil {
		t.Fatal("locked patch hid bad config")
	}
	for _, config := range []string{`null`, `[]`, `{"deletionTimeout":"invalid"}`, `{"reminderTimeouts":[null]}`, `{"promotionStrategy":"unknown"}`, `{"reminderTimeoutDurations":[1]}`} {
		row.Config = json.RawMessage(config)
		if _, err := resolveAdminlessFeature(defaults, row); err == nil {
			t.Fatalf("bad patch accepted %s", config)
		}
	}
	for _, raw := range []string{`{}`, `{"defaults":null}`, `{"defaults":{"status":"enabled","lockStatus":"unlocked","config":{}}}`, `{"defaults":{"status":"enabled","lockStatus":"unlocked","config":{"promotionStrategy":"all","deletionTimeoutDuration":"1s","deletionTimeout":"bad","reminderTimeoutDurations":[]}}}`} {
		if _, err := decodeAdminlessDefaults(json.RawMessage(raw)); err == nil {
			t.Fatalf("invalid defaults accepted: %s", raw)
		}
	}
}
func TestAdminlessDefaultsFromGalleyYAML(t *testing.T) {
	var galley GalleyConfig
	if err := yaml.Unmarshal([]byte("settings:\n  federationDomain: wire.com\n  featureFlags:\n    preventAdminlessGroups:\n      defaults:\n        status: enabled\n        lockStatus: unlocked\n        config:\n          promotionStrategy: alphabetical\n          deletionTimeoutDuration: 7d\n          reminderTimeoutDurations: [2d, 4d, 6d]\n"), &galley); err != nil {
		t.Fatal(err)
	}
	defaults, err := galley.adminlessDefaults()
	if err != nil || !defaults.Feature.Enabled || len(defaults.Feature.ReminderPico) != 3 {
		t.Fatalf("Galley config %v %v", defaults, err)
	}
}

func TestAdminlessLegacyWordBoundsWithoutHugeAllocation(t *testing.T) {
	for _, text := range []string{"1.000e0", "10e-1", "0e999999999", "18446744073709551615"} {
		if _, err := adminlessLegacyDays(json.RawMessage(text)); err != nil {
			t.Fatalf("native Word %s: %v", text, err)
		}
	}
	for _, text := range []string{"1e999999999", "1e-999999999", "18446744073709551616", "-1", "1.1", "null", "true", `"1"`} {
		if _, err := adminlessLegacyDays(json.RawMessage(text)); err == nil {
			t.Fatalf("invalid Word %s", text)
		}
	}
}
