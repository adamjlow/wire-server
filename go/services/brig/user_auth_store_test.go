// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestUserStorageDiagnosticsExcludePrivateValues(t *testing.T) {
	private := "synthetic-private-field"
	for _, original := range []error{
		errors.New(private),
		pgx.ScanArgError{ColumnIndex: 6, FieldName: private, Err: errors.New(private)},
	} {
		err := userStorageError(original)
		if err == nil || strings.Contains(err.Error(), private) || errors.Is(err, original) {
			t.Fatal("storage diagnostic retained private error material")
		}
	}
	err := userStorageError(pgx.ScanArgError{ColumnIndex: 6, Err: errors.New(private)})
	if err.Error() != "user storage decoding failed at column 6" || userStorageError(nil) != nil {
		t.Fatal("safe decoding location or success changed")
	}
}
