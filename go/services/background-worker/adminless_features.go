// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"bytes"
	"encoding/json"
	"errors"
	"math/big"
	"regexp"
	"strings"
)

const adminlessFeatureName = "preventAdminlessGroups"

type adminlessDefaults struct {
	Feature adminlessFeature
	Locked  bool
}

func defaultAdminlessFeature() adminlessDefaults {
	day := big.NewInt(86400000000000000)
	return adminlessDefaults{Feature: adminlessFeature{Strategy: "alphabetical", DeletionPico: new(big.Int).Mul(day, big.NewInt(7)), ReminderPico: []*big.Int{new(big.Int).Mul(day, big.NewInt(2)), new(big.Int).Mul(day, big.NewInt(4)), new(big.Int).Mul(day, big.NewInt(6))}}}
}

var adminlessDurationPattern = regexp.MustCompile(`^([+-]?)([0-9]+)(?:\.([0-9]*))?(?:[eE]([+-]?[0-9]+))?(us|ms|s|m|h|d|w)$`)

func invalidAdminlessFeature() error { return errors.New("invalid adminless feature") }

// Native Atto.rational parses Scientific into fixed Pico BEFORE multiplying the
// fixed unit. Keep signed floor, legacy Word64 day bound and sub-pico behavior;
// Go time.Duration would impose a new ~292-year limit on accepted settings.
func parseAdminlessDuration(text string) (*big.Int, error) {
	m := adminlessDurationPattern.FindStringSubmatch(text)
	if m == nil {
		return nil, invalidAdminlessFeature()
	}
	digits := strings.TrimLeft(m[2]+m[3], "0")
	if digits == "" {
		return new(big.Int), nil
	}
	exponent := int64(-len(m[3]))
	if m[4] != "" {
		e, ok := new(big.Int).SetString(strings.TrimPrefix(m[4], "+"), 10)
		if !ok {
			return nil, invalidAdminlessFeature()
		}
		exponent += e.Int64()
	}
	// Scientific's exponent is native Int; addition and decimal parse wrap at64
	// bits. Interpret that value before deciding whether a power needs allocating.
	// Avoid overflow when adding12 to native exponent: represent subsequent fixed
	// scaling with big integers, unlike Scientific's wrapped exponent itself.
	power := new(big.Int).Add(big.NewInt(exponent), big.NewInt(12))
	order := new(big.Int).Add(power, big.NewInt(int64(len(digits))))
	negative := m[1] == "-"
	var pico *big.Int
	if order.Sign() <= 0 {
		pico = new(big.Int)
		if negative {
			pico.SetInt64(-1)
		}
	} else {
		if order.Cmp(big.NewInt(43)) > 0 {
			return nil, invalidAdminlessFeature()
		}
		coefficient, _ := new(big.Int).SetString(digits, 10)
		if negative {
			coefficient.Neg(coefficient)
		}
		if power.Sign() >= 0 {
			pico = new(big.Int).Mul(coefficient, new(big.Int).Exp(big.NewInt(10), power, nil))
		} else {
			pico = new(big.Int).Div(coefficient, new(big.Int).Exp(big.NewInt(10), new(big.Int).Neg(power), nil))
		}
	}
	unit := map[string]int64{"us": 1000000, "ms": 1000000000, "s": 1000000000000, "m": 60000000000000, "h": 3600000000000000, "d": 86400000000000000, "w": 604800000000000000}[m[5]]
	pico.Mul(pico, big.NewInt(unit))
	pico.Div(pico, big.NewInt(1000000000000))
	if pico.Sign() < 0 {
		return nil, invalidAdminlessFeature()
	}
	max := new(big.Int).Mul(new(big.Int).SetUint64(^uint64(0)), big.NewInt(86400000000000000))
	if pico.Cmp(max) > 0 {
		return nil, invalidAdminlessFeature()
	}
	return pico, nil
}

var adminlessWordPattern = regexp.MustCompile(`^(-?)([0-9]+)(?:\.([0-9]+))?(?:[eE]([+-]?[0-9]+))?$`)

func adminlessLegacyDays(raw json.RawMessage) (*big.Int, error) {
	// Parse bounded Word without first materializing a possibly enormous rational.
	// JSON valid-number grammar and exact integral/range checks preserve the API.
	if !json.Valid(raw) {
		return nil, invalidAdminlessFeature()
	}
	m := adminlessWordPattern.FindStringSubmatch(string(bytes.TrimSpace(raw)))
	if m == nil {
		return nil, invalidAdminlessFeature()
	}
	digits := strings.TrimLeft(m[2]+m[3], "0")
	if digits == "" {
		return new(big.Int), nil
	}
	if m[1] == "-" {
		return nil, invalidAdminlessFeature()
	}
	power := big.NewInt(int64(-len(m[3])))
	if m[4] != "" {
		e, ok := new(big.Int).SetString(strings.TrimPrefix(m[4], "+"), 10)
		if !ok {
			return nil, invalidAdminlessFeature()
		}
		power.Add(power, e)
	}
	trimmed := strings.TrimRight(digits, "0")
	power.Add(power, big.NewInt(int64(len(digits)-len(trimmed))))
	if power.Sign() < 0 || power.Cmp(big.NewInt(20)) > 0 || int64(len(trimmed))+power.Int64() > 20 {
		return nil, invalidAdminlessFeature()
	}
	days, _ := new(big.Int).SetString(trimmed, 10)
	days.Mul(days, new(big.Int).Exp(big.NewInt(10), power, nil))
	if days.BitLen() > 64 {
		return nil, invalidAdminlessFeature()
	}
	return days.Mul(days, big.NewInt(86400000000000000)), nil
}
func adminlessObject(raw json.RawMessage) (map[string]json.RawMessage, error) {
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || object == nil {
		return nil, invalidAdminlessFeature()
	}
	return object, nil
}
func adminlessPresent(raw json.RawMessage) bool {
	return raw != nil && string(bytes.TrimSpace(raw)) != "null"
}
func adminlessText(raw json.RawMessage) (string, error) {
	var value string
	if !adminlessPresent(raw) || json.Unmarshal(raw, &value) != nil {
		return "", invalidAdminlessFeature()
	}
	return value, nil
}
func applyAdminlessConfig(base adminlessFeature, raw json.RawMessage, required bool) (adminlessFeature, error) {
	obj, err := adminlessObject(raw)
	if err != nil {
		return base, err
	}
	if adminlessPresent(obj["promotionStrategy"]) {
		strategy, err := adminlessText(obj["promotionStrategy"])
		if err != nil {
			return base, err
		}
		switch strategy {
		case "alphabetical", "random", "all":
			base.Strategy = strategy
		default:
			return base, invalidAdminlessFeature()
		}
	} else if required {
		return base, invalidAdminlessFeature()
	}
	parseTimeout := func(legacy, modern string) (*big.Int, bool, error) {
		var old, current *big.Int
		if adminlessPresent(obj[legacy]) {
			var err error
			old, err = adminlessLegacyDays(obj[legacy])
			if err != nil {
				return nil, false, err
			}
		}
		if adminlessPresent(obj[modern]) {
			text, err := adminlessText(obj[modern])
			if err != nil {
				return nil, false, err
			}
			current, err = parseAdminlessDuration(text)
			if err != nil {
				return nil, false, err
			}
		}
		if current != nil {
			return current, true, nil
		}
		// Stored legacy fields are validated then intentionally ignored by native
		// dbPreventAdminlessTimeoutField; only server/public schema uses them.
		if required && old != nil {
			return old, true, nil
		}
		return nil, false, nil
	}
	value, present, err := parseTimeout("deletionTimeout", "deletionTimeoutDuration")
	if err != nil {
		return base, err
	}
	if present {
		base.DeletionPico = value
	} else if required {
		return base, invalidAdminlessFeature()
	}
	parseTimeouts := func(field string, legacy bool) ([]*big.Int, bool, error) {
		if !adminlessPresent(obj[field]) {
			return nil, false, nil
		}
		var raws []json.RawMessage
		if json.Unmarshal(obj[field], &raws) != nil {
			return nil, false, invalidAdminlessFeature()
		}
		values := make([]*big.Int, 0, len(raws))
		for _, raw := range raws {
			var v *big.Int
			var err error
			if legacy {
				v, err = adminlessLegacyDays(raw)
			} else {
				text, e := adminlessText(raw)
				if e != nil {
					return nil, false, e
				}
				v, err = parseAdminlessDuration(text)
			}
			if err != nil {
				return nil, false, err
			}
			values = append(values, v)
		}
		return values, true, nil
	}
	old, oldPresent, err := parseTimeouts("reminderTimeouts", true)
	if err != nil {
		return base, err
	}
	values, present, err := parseTimeouts("reminderTimeoutDurations", false)
	if err != nil {
		return base, err
	}
	if present {
		base.ReminderPico = values
	} else if required && oldPresent {
		base.ReminderPico = old
	} else if required {
		return base, invalidAdminlessFeature()
	}
	return base, nil
}
func decodeAdminlessDefaults(raw json.RawMessage) (adminlessDefaults, error) {
	defaults := defaultAdminlessFeature()
	if !adminlessPresent(raw) {
		return defaults, nil
	}
	obj, err := adminlessObject(raw)
	if err != nil {
		return defaults, err
	}
	obj, err = adminlessObject(obj["defaults"])
	if err != nil {
		return defaults, err
	}
	if adminlessPresent(obj["ttl"]) {
		text, textErr := adminlessText(obj["ttl"])
		if textErr != nil || (text != "unlimited" && text != "0") {
			value, err := adminlessLegacyDays(obj["ttl"])
			if err != nil || value.Sign() == 0 {
				return defaults, invalidAdminlessFeature()
			}
		}
	}
	status, err := adminlessText(obj["status"])
	if err != nil {
		return defaults, err
	}
	switch status {
	case "enabled":
		defaults.Feature.Enabled = true
	case "disabled":
		defaults.Feature.Enabled = false
	default:
		return defaults, invalidAdminlessFeature()
	}
	lock, err := adminlessText(obj["lockStatus"])
	if err != nil {
		return defaults, err
	}
	switch lock {
	case "locked":
		defaults.Locked = true
	case "unlocked":
		defaults.Locked = false
	default:
		return defaults, invalidAdminlessFeature()
	}
	defaults.Feature, err = applyAdminlessConfig(defaults.Feature, obj["config"], true)
	return defaults, err
}
func resolveAdminlessFeature(defaults adminlessDefaults, row *featureMigrationRow) (adminlessFeature, error) {
	result := defaults.Feature
	locked := defaults.Locked
	if row != nil {
		if row.Status != nil {
			if *row.Status != 0 && *row.Status != 1 {
				return result, invalidAdminlessFeature()
			}
			result.Enabled = *row.Status == 1
		}
		if row.LockStatus != nil {
			if *row.LockStatus != 0 && *row.LockStatus != 1 {
				return result, invalidAdminlessFeature()
			}
			locked = *row.LockStatus == 0
		}
		if row.Config != nil {
			var err error
			result, err = applyAdminlessConfig(result, row.Config, false)
			if err != nil {
				return result, err
			}
		}
	}
	// Native parses the whole patch even when its effective lock forces defaults.
	if locked {
		return defaults.Feature, nil
	}
	return result, nil
}
