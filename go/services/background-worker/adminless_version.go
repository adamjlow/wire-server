// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"bytes"
	"encoding/json"
	"errors"
	"math/big"
	"strconv"
	"strings"
)

// Schema Int accepts exact integral JSON Scientific values, including decimal/
// exponent forms and unknown future versions. Bound before expanding exponents.
func adminlessVersionInteger(raw json.RawMessage) (int64, bool) {
	m := adminlessWordPattern.FindStringSubmatch(string(bytes.TrimSpace(raw)))
	if m == nil {
		return 0, false
	}
	digits := strings.TrimLeft(m[2]+m[3], "0")
	if digits == "" {
		return 0, true
	}
	power := big.NewInt(int64(-len(m[3])))
	if m[4] != "" {
		exp, ok := new(big.Int).SetString(strings.TrimPrefix(m[4], "+"), 10)
		if !ok {
			return 0, false
		}
		power.Add(power, exp)
	}
	trimmed := strings.TrimRight(digits, "0")
	power.Add(power, big.NewInt(int64(len(digits)-len(trimmed))))
	if power.Sign() < 0 || power.Cmp(big.NewInt(19)) > 0 || int64(len(trimmed))+power.Int64() > 19 {
		return 0, false
	}
	value := trimmed + strings.Repeat("0", int(power.Int64()))
	if m[1] == "-" {
		value = "-" + value
	}
	v, err := strconv.ParseInt(value, 10, 64)
	return v, err == nil
}

func adminlessVersionInfo(body []byte) ([]int64, error) {
	invalid := errors.New("invalid federation version response")
	var info map[string]json.RawMessage
	if !validAdminlessAccountJSONText(body) || json.Unmarshal(body, &info) != nil || info == nil {
		return nil, invalid
	}
	var legacy []json.RawMessage
	raw := bytes.TrimSpace(info["supported"])
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) || json.Unmarshal(raw, &legacy) != nil {
		return nil, invalid
	}
	for _, v := range legacy {
		if _, ok := adminlessVersionInteger(v); !ok {
			return nil, invalid
		}
	}
	raw = bytes.TrimSpace(info["supported_versions"])
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return []int64{0}, nil
	}
	var modern []json.RawMessage
	if json.Unmarshal(raw, &modern) != nil {
		return nil, invalid
	}
	result := make([]int64, 0, len(modern))
	for _, raw := range modern {
		v, ok := adminlessVersionInteger(raw)
		if !ok {
			return nil, invalid
		}
		result = append(result, v)
	}
	return result, nil
}
