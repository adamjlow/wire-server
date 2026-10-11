// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"strings"
	"testing"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func TestPasswordResetKeyAndRandomCodeNativeEncoding(t *testing.T) {
	if key := passwordResetKey(gocql.UUID{}); key != "Erk3fL5-XJTopw2dI5KVI9FK-pVHkxMPijlZx7hJrKg=" {
		t.Fatal("UUID text hash changed", key)
	}
	code, err := newPasswordResetCode()
	if err != nil || len(code) != 32 || !validActivationValue(code) || strings.Contains(code, "=") {
		t.Fatal("native24-byte code shape changed", err)
	}
	if strings.Contains((passwordResetRecord{code: code}).String(), code) {
		t.Fatal("password reset record exposed credentials")
	}
}
