// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"github.com/zeebo/xxh3"
	"golang.org/x/text/cases"
)

// The reference uses hashable-1.5.1.0 with text>=2: Text.hash is XXH3_64
// over UTF-8 bytes, with seed0, then converts the Word64 to signed Int64.
// It is not FNV, XXH64 or hashWithSalt. Locks must match live Haskell peers.
func textMigrationLockKey(value string) int64 { return int64(xxh3.HashString(value)) }

func foldedDomainKey(value string) string { return cases.Fold().String(value) }

type migrationLockTimeout struct{}

func (*migrationLockTimeout) Error() string { return "migration lock acquisition failed" }
