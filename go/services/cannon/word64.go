package cannon

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

// Aeson Word64 accepts JSON numbers whose value is an in-range integer, including
// decimal/exponent notation. Preserve exact precision without floats or big
// powers; work and temporary storage stay linear in the received input size.
func decodeWord64(raw []byte) (uint64, error) {
	bad := errors.New("invalid unsigned integer")
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || !json.Valid(raw) || (raw[0] != '-' && (raw[0] < '0' || raw[0] > '9')) {
		return 0, bad
	}
	digits := string(raw)
	negative := digits[0] == '-'
	if negative {
		digits = digits[1:]
	}
	var exponent int64
	if index := strings.IndexAny(digits, "eE"); index >= 0 {
		var err error
		exponent, err = strconv.ParseInt(digits[index+1:], 10, 64)
		if err != nil {
			return 0, bad
		}
		digits = digits[:index]
	}
	var fractionDigits int64
	if index := strings.IndexByte(digits, '.'); index >= 0 {
		fractionDigits = int64(len(digits) - index - 1)
		digits = digits[:index] + digits[index+1:]
	}
	digits = strings.TrimLeft(digits, "0")
	if digits == "" {
		return 0, nil
	}
	if negative || exponent > fractionDigits+20 || exponent < fractionDigits-int64(len(digits)) {
		return 0, bad
	}
	// The preceding bounds also prevent overflowing this subtraction.
	scale := exponent - fractionDigits
	if scale < 0 {
		cut := len(digits) + int(scale)
		if strings.Trim(digits[cut:], "0") != "" {
			return 0, bad
		}
		digits = digits[:cut]
	} else {
		if int64(len(digits))+scale > 20 {
			return 0, bad
		}
		digits += strings.Repeat("0", int(scale))
	}
	return strconv.ParseUint(digits, 10, 64)
}
