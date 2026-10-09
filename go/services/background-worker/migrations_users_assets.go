// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/apache/cassandra-gocql-driver/v2"
)

// PotentiallyInvalidAsset in the reference distinguishes absent UDT fields.
// gocql's default UDT map scan turns NULL numbers into zero; for assets this
// would invent a valid type0/preview0 instead of skipping an invalid asset.
type userCassandraAsset struct {
	Type, Size *int32
	Key        *string
}

func (a *userCassandraAsset) UnmarshalUDT(name string, info gocql.TypeInfo, data []byte) error {
	if data == nil {
		return nil
	}
	switch name {
	case "typ", "size":
		var value int32
		if err := gocql.Unmarshal(info, data, &value); err != nil {
			return errors.New("invalid stored asset integer")
		}
		if name == "typ" {
			a.Type = &value
		} else {
			a.Size = &value
		}
	case "key":
		var value string
		if err := gocql.Unmarshal(info, data, &value); err != nil || !utf8.ValidString(value) {
			return errors.New("invalid stored asset key text")
		}
		a.Key = &value
	}
	return nil
}

type nullableUserMigrationAssets struct{ Value []userCassandraAsset }

func (a *nullableUserMigrationAssets) UnmarshalCQL(info gocql.TypeInfo, data []byte) error {
	if data == nil {
		a.Value = nil
		return nil
	}
	var value []userCassandraAsset
	if err := gocql.Unmarshal(info, data, &value); err != nil {
		return errors.New("invalid stored user asset collection")
	}
	if value == nil {
		value = []userCassandraAsset{}
	}
	a.Value = value
	return nil
}

func canonicalUserAssetKey(value string) (string, bool) {
	version, remainder, ok := strings.Cut(value, "-")
	if !ok {
		return "", false
	}
	retention, id, ok := strings.Cut(remainder, "-")
	if !ok {
		return "", false
	}
	v, ok := nativeAssetWord(version)
	if !ok || v != 3 {
		return "", false
	}
	r, ok := nativeAssetWord(retention)
	if !ok || r < 1 || r > 5 {
		return "", false
	}
	u, err := parseUserID(id)
	if err != nil {
		return "", false
	}
	return "3-" + strconv.FormatUint(r, 10) + "-" + u.String(), true
}

func nativeAssetWord(value string) (uint64, bool) {
	if value == "" {
		return 0, false
	}
	var result uint64
	for _, c := range value {
		if c < '0' || c > '9' {
			return 0, false
		}
		result = result*10 + uint64(c-'0')
	}
	return result, true // Native decimal Word arithmetic wraps at 64 bits.
}

func filterUserMigrationAssets(input []userCassandraAsset) []userMigrationAsset {
	if input == nil {
		return nil
	}
	output := make([]userMigrationAsset, 0, len(input))
	for _, asset := range input {
		valid := asset.Type != nil && *asset.Type == 0 && asset.Key != nil && (asset.Size == nil || *asset.Size == 0 || *asset.Size == 1)
		key := ""
		if valid {
			key, valid = canonicalUserAssetKey(*asset.Key)
		}
		if !valid {
			// Source drops only the bad asset, preserving the user migration.
			// Do not reproduce its raw asset/user identifier diagnostic logging.
			slog.Warn("invalid stored user asset omitted from migration")
			continue
		}
		output = append(output, userMigrationAsset{Key: key, Size: asset.Size})
	}
	return output
}
