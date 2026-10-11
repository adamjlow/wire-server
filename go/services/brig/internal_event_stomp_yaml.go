// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

var stompDecimal = regexp.MustCompile(`^[+-]?[0-9]+(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$`)

// Data.Yaml first creates Aeson values; decoding directly into Go strings/ints
// would coerce numbers into credentials and truncate fractional ports.
func stompNativeValue(n *yaml.Node, depth int, budget *int) (any, error) {
	*budget--
	if *budget < 0 {
		return nil, errors.New("invalid internal STOMP YAML")
	}
	if n == nil || depth > 64 {
		return nil, errors.New("invalid internal STOMP YAML")
	}
	if n.Kind == yaml.AliasNode {
		return stompNativeValue(n.Alias, depth+1, budget)
	}
	if n.Kind == yaml.MappingNode {
		return stompNativeFieldsBudget(n, depth+1, budget)
	}
	if n.Kind == yaml.SequenceNode {
		values := make([]any, 0, len(n.Content))
		for _, child := range n.Content {
			value, err := stompNativeValue(child, depth+1, budget)
			if err != nil {
				return nil, err
			}
			values = append(values, value)
		}
		return values, nil
	}
	if n.Kind != yaml.ScalarNode {
		return nil, errors.New("invalid internal STOMP YAML")
	}
	value := n.Value
	if n.Style&(yaml.SingleQuotedStyle|yaml.DoubleQuotedStyle|yaml.FoldedStyle) != 0 || n.Tag == "!!str" && n.Style&yaml.TaggedStyle != 0 {
		return value, nil
	}
	switch value {
	case "", "~", "null", "Null", "NULL":
		return nil, nil
	case "y", "Y", "yes", "Yes", "YES", "on", "On", "ON", "true", "True", "TRUE":
		return true, nil
	case "n", "N", "no", "No", "NO", "off", "Off", "OFF", "false", "False", "FALSE":
		return false, nil
	}
	if stompDecimal.MatchString(value) {
		return json.Number(value), nil
	}
	for _, prefix := range []string{"0x", "0o"} {
		if strings.HasPrefix(value, prefix) && len(value) > 2 {
			base := 16
			if prefix == "0o" {
				base = 8
			}
			if _, err := strconv.ParseUint(value[2:], base, 64); err == nil {
				return json.Number(value), nil
			}
			// A numeric overflow is still a Number, so Text must reject it.
			valid := true
			for _, r := range value[2:] {
				if base == 8 {
					valid = valid && r >= '0' && r <= '7'
				} else {
					valid = valid && (r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F')
				}
			}
			if valid {
				return json.Number(value), nil
			}
		}
	}
	return value, nil
}

func stompNativeFields(n *yaml.Node, depth int) (map[string]any, error) {
	budget := 65536
	return stompNativeFieldsBudget(n, depth, &budget)
}

func stompNativeFieldsBudget(n *yaml.Node, depth int, budget *int) (map[string]any, error) {
	*budget--
	if *budget < 0 {
		return nil, errors.New("invalid internal STOMP YAML")
	}
	if n == nil || depth > 64 {
		return nil, errors.New("invalid internal STOMP YAML")
	}
	if n.Kind == yaml.AliasNode {
		return stompNativeFieldsBudget(n.Alias, depth+1, budget)
	}
	if n.Kind != yaml.MappingNode {
		return nil, errors.New("invalid internal STOMP YAML")
	}
	fields := make(map[string]any)
	for i := 0; i+1 < len(n.Content); i += 2 {
		key := n.Content[i]
		name := key.Value
		if key.Kind == yaml.AliasNode {
			value, err := stompNativeValue(key, depth+1, budget)
			if err != nil {
				return nil, err
			}
			var ok bool
			name, ok = value.(string)
			if !ok {
				return nil, errors.New("invalid internal STOMP YAML")
			}
		} else if key.Kind != yaml.ScalarNode {
			return nil, errors.New("invalid internal STOMP YAML")
		}
		value, err := stompNativeValue(n.Content[i+1], depth+1, budget)
		if err != nil {
			return nil, err
		}
		if name == "<<" {
			merge := func(values map[string]any) {
				for k, v := range values {
					if _, exists := fields[k]; !exists {
						fields[k] = v
					}
				}
			}
			switch values := value.(type) {
			case map[string]any:
				merge(values)
			case []any:
				for _, item := range values {
					if m, ok := item.(map[string]any); ok {
						merge(m)
					}
				}
			default:
				fields[name] = value
			}
		} else {
			fields[name] = value
		}
	}
	return fields, nil
}

// Convert Scientific to a signed machine Int exactly, without allocating a
// giant power of ten for an attacker-controlled exponent.
func stompNativeInteger(text string) (int, error) {
	invalid := errors.New("invalid internal STOMP integer")
	if strings.HasPrefix(text, "0x") || strings.HasPrefix(text, "0o") {
		base := 16
		if text[1] == 'o' {
			base = 8
		}
		n, err := strconv.ParseInt(text[2:], base, strconv.IntSize)
		return int(n), err
	}
	negative := strings.HasPrefix(text, "-")
	text = strings.TrimPrefix(strings.TrimPrefix(text, "-"), "+")
	exponent := int64(0)
	if i := strings.IndexAny(text, "eE"); i >= 0 {
		var err error
		exponent, err = strconv.ParseInt(text[i+1:], 10, 64)
		if err != nil {
			return 0, invalid
		}
		text = text[:i]
	}
	whole, fraction, _ := strings.Cut(text, ".")
	digits := strings.TrimLeft(whole+fraction, "0")
	if digits == "" {
		return 0, nil
	}
	// A nonzero number needs at most len(digits) decimal cancellations and
	// at most19 significant digits for a signed64-bit Int.
	if exponent < -int64(len(digits)) || exponent > int64(len(fraction))+19 {
		return 0, invalid
	}
	scale := exponent - int64(len(fraction))
	if scale < 0 {
		remove := -scale
		if remove >= int64(len(digits)) {
			return 0, invalid
		}
		cut := len(digits) - int(remove)
		if strings.Trim(digits[cut:], "0") != "" {
			return 0, invalid
		}
		digits = digits[:cut]
	} else {
		if int64(len(digits))+scale > 19 {
			return 0, invalid
		}
		digits += strings.Repeat("0", int(scale))
	}
	if negative {
		digits = "-" + digits
	}
	n, err := strconv.ParseInt(digits, 10, strconv.IntSize)
	return int(n), err
}
