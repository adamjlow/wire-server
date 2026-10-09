// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"errors"
	"math"
	"unicode/utf8"
)

func invalidUserMigrationRow() error { return errors.New("invalid stored user migration row") }

func userMigrationString(value any) (string, error) {
	text, ok := value.(string)
	if !ok || !utf8.ValidString(text) {
		return "", invalidUserMigrationRow()
	}
	return text, nil
}

func userMigrationNullableString(value any) (*string, error) {
	if value == nil {
		return nil, nil
	}
	text, err := userMigrationString(value)
	if err != nil {
		return nil, err
	}
	return &text, nil
}

func userMigrationNullableInt32(value any) (*int32, error) {
	if value == nil {
		return nil, nil
	}
	n, ok := cqlInteger(value)
	if !ok || n < math.MinInt32 || n > math.MaxInt32 {
		return nil, invalidUserMigrationRow()
	}
	i := int32(n)
	return &i, nil
}
